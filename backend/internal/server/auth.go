package server

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/pbkdf2"
	_ "golang.org/x/image/webp"

	"semanticatlas/backend/internal/analysis"
	"semanticatlas/backend/internal/store"
)

const (
	cookieName     = "atlas_session"
	sessionSeconds = 7 * 24 * 60 * 60
	pbkdf2Rounds   = 260000
)

func uid() string { return uuid.NewString() }

// hashPassword uses the same format as the Python version: pbkdf2_sha256$rounds$salt$digest.
func hashPassword(password string) string {
	salt := make([]byte, 16)
	rand.Read(salt)
	digest := pbkdf2.Key([]byte(password), salt, pbkdf2Rounds, 32, sha256.New)
	return fmt.Sprintf("pbkdf2_sha256$%d$%s$%s", pbkdf2Rounds, hex.EncodeToString(salt), hex.EncodeToString(digest))
}

func verifyPassword(password, hashed string) bool {
	parts := strings.Split(hashed, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2_sha256" {
		return false
	}
	rounds, err := strconv.Atoi(parts[1])
	if err != nil || rounds < 1 || rounds > 10_000_000 {
		return false
	}
	salt, err1 := hex.DecodeString(parts[2])
	expected, err2 := hex.DecodeString(parts[3])
	if err1 != nil || err2 != nil {
		return false
	}
	actual := pbkdf2.Key([]byte(password), salt, rounds, len(expected), sha256.New)
	return hmac.Equal(actual, expected)
}

type ctxKey struct{}

// authed resolves the session cookie, checks server-side revocation and passes the user on.
func (s *Server) authed(next func(http.ResponseWriter, *http.Request, *store.User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(cookieName)
		if err != nil || cookie.Value == "" {
			fail(w, http.StatusUnauthorized, "Войдите в аккаунт.")
			return
		}
		user, err := s.userFromToken(r.Context(), cookie.Value)
		if err != nil {
			fail(w, http.StatusUnauthorized, "Сеанс завершён. Войдите снова.")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, user)), user)
	}
}

func (s *Server) parseToken(raw string, validate bool) (jwt.MapClaims, error) {
	opts := []jwt.ParserOption{jwt.WithValidMethods([]string{"HS256"})}
	if validate {
		opts = append(opts, jwt.WithExpirationRequired())
	} else {
		opts = append(opts, jwt.WithoutClaimsValidation())
	}
	token, err := jwt.Parse(raw, func(*jwt.Token) (any, error) { return s.secret, nil }, opts...)
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.New("claims")
	}
	return claims, nil
}

func (s *Server) userFromToken(ctx context.Context, raw string) (*store.User, error) {
	claims, err := s.parseToken(raw, true)
	if err != nil {
		return nil, err
	}
	sub, _ := claims["sub"].(string)
	jti, _ := claims["jti"].(string)
	if sub == "" || jti == "" {
		return nil, errors.New("missing claims")
	}
	userID, expires, err := s.store.Session(ctx, jti)
	if err != nil || userID != sub || expires < time.Now().Unix() {
		return nil, errors.New("revoked")
	}
	return s.store.UserByID(ctx, sub)
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, user *store.User) error {
	now := time.Now().Unix()
	sid := uid()
	if err := s.store.CreateSession(r.Context(), sid, user.ID, now+sessionSeconds); err != nil {
		return err
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": user.ID, "jti": sid, "iat": now, "exp": now + sessionSeconds}).SignedString(s.secret)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: token, MaxAge: sessionSeconds, HttpOnly: true, Secure: s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode, Path: "/"})
	return nil
}

// field reads an optional string field; ok is false when the JSON type is wrong.
func field(body map[string]json.RawMessage, key string) (value string, present, ok bool) {
	raw, present := body[key]
	if !present || string(raw) == "null" {
		return "", present, true
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", true, false
	}
	return value, true, true
}

func runeLen(s string) int { return utf8.RuneCountInString(s) }

var loginRe = regexp.MustCompile(`^[a-zа-яё0-9_.@-]{3,80}$`)

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	if !s.limits.allow(r, "register", 25) {
		fail(w, http.StatusTooManyRequests, "Слишком много попыток. Подождите пять минут.")
		return
	}
	body, ok := decode(w, r)
	if !ok {
		return
	}
	login, _, ok1 := field(body, "login")
	name, _, ok2 := field(body, "name")
	password, _, ok3 := field(body, "password")
	if !ok1 || !ok2 || !ok3 {
		fail(w, http.StatusUnprocessableEntity, "Проверьте поля формы.")
		return
	}
	login, name = strings.ToLower(strings.TrimSpace(login)), strings.TrimSpace(name)
	switch {
	case !loginRe.MatchString(login):
		fail(w, http.StatusUnprocessableEntity, "Логин: от 3 до 80 букв, цифр или символов _ . @ -")
	case name == "" || runeLen(name) > 100:
		fail(w, http.StatusUnprocessableEntity, "Укажите имя (до 100 символов).")
	case runeLen(password) < 8 || runeLen(password) > 128:
		fail(w, http.StatusUnprocessableEntity, "Пароль: от 8 до 128 символов.")
	default:
		user := &store.User{ID: uid(), Login: login, Name: name, PasswordHash: hashPassword(password), Language: "ru", Private: true}
		if err := s.store.CreateUser(r.Context(), user); err != nil {
			if errors.Is(err, store.ErrConflict) {
				fail(w, http.StatusConflict, "Этот логин уже занят.")
				return
			}
			serverError(w, err)
			return
		}
		if err := s.startSession(w, r, user); err != nil {
			serverError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"user": user})
	}
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if !s.limits.allow(r, "login", 25) {
		fail(w, http.StatusTooManyRequests, "Слишком много попыток. Подождите пять минут.")
		return
	}
	body, ok := decode(w, r)
	if !ok {
		return
	}
	login, _, ok1 := field(body, "login")
	password, _, ok2 := field(body, "password")
	if !ok1 || !ok2 || login == "" || password == "" || runeLen(login) > 80 || runeLen(password) > 128 {
		fail(w, http.StatusUnprocessableEntity, "Укажите логин и пароль.")
		return
	}
	user, err := s.store.UserByLogin(r.Context(), strings.ToLower(strings.TrimSpace(login)))
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		serverError(w, err)
		return
	}
	if user == nil || !verifyPassword(password, user.PasswordHash) {
		fail(w, http.StatusUnauthorized, "Неверный логин или пароль.")
		return
	}
	if err := s.startSession(w, r, user); err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": user})
}

func (s *Server) demo(w http.ResponseWriter, r *http.Request) {
	if !s.limits.allow(r, "demo", 15) {
		fail(w, http.StatusTooManyRequests, "Слишком много попыток. Подождите пять минут.")
		return
	}
	secret := make([]byte, 24)
	rand.Read(secret)
	suffix := make([]byte, 8)
	rand.Read(suffix)
	user := &store.User{ID: uid(), Login: "demo_" + hex.EncodeToString(suffix), Name: "Исследователь",
		PasswordHash: hashPassword(base64.RawURLEncoding.EncodeToString(secret)), Language: "ru", Private: true}
	if err := s.store.CreateUser(r.Context(), user); err != nil {
		serverError(w, err)
		return
	}
	year := 2026
	doc, err := s.saveDocument(r.Context(), user, demoTitle, demoAuthor, &year, "Учебный рассказ", demoText)
	if err != nil {
		serverError(w, err)
		return
	}
	if err := s.startSession(w, r, user); err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": user, "document_id": doc.ID})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request, user *store.User) {
	writeJSON(w, http.StatusOK, user)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(cookieName); err == nil {
		if claims, err := s.parseToken(cookie.Value, false); err == nil {
			if jti, _ := claims["jti"].(string); jti != "" {
				s.store.DeleteSession(r.Context(), jti)
			}
		}
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", MaxAge: -1, Path: "/", HttpOnly: true, Secure: s.cfg.CookieSecure, SameSite: http.SameSiteLaxMode})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

var avatarRe = regexp.MustCompile(`^data:image/(png|jpeg|webp);base64,([A-Za-z0-9+/=\r\n]+)$`)

func validAvatar(avatar string) bool {
	m := avatarRe.FindStringSubmatch(avatar)
	if m == nil || len(avatar) > 700000 {
		return false
	}
	raw, err := base64.StdEncoding.DecodeString(strings.NewReplacer("\r", "", "\n", "").Replace(m[2]))
	if err != nil || len(raw) > 500000 {
		return false
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || cfg.Width > 3000 || cfg.Height > 3000 || (format != "png" && format != "jpeg" && format != "webp") {
		return false
	}
	_, _, err = image.Decode(bytes.NewReader(raw))
	return err == nil
}

func (s *Server) updateProfile(w http.ResponseWriter, r *http.Request, user *store.User) {
	body, ok := decode(w, r)
	if !ok {
		return
	}
	if name, present, ok := field(body, "name"); present {
		if !ok || strings.TrimSpace(name) == "" || runeLen(name) > 100 {
			fail(w, http.StatusUnprocessableEntity, "Имя не может быть пустым.")
			return
		}
		user.Name = strings.TrimSpace(name)
	}
	if lang, present, ok := field(body, "language"); present && string(body["language"]) != "null" {
		if !ok || lang != "ru" {
			fail(w, http.StatusUnprocessableEntity, "Сейчас доступен русский интерфейс (ru).")
			return
		}
	}
	if raw, present := body["private"]; present {
		var private bool
		if string(raw) == "null" || json.Unmarshal(raw, &private) != nil {
			fail(w, http.StatusUnprocessableEntity, "Укажите настройку приватности.")
			return
		}
		user.Private = private
	}
	if avatar, present, ok := field(body, "avatar"); present {
		switch {
		case !ok:
			fail(w, http.StatusUnprocessableEntity, "Фото должно быть PNG, JPEG или WebP до 500 КБ.")
			return
		case avatar == "":
			user.Avatar = nil
		case !avatarRe.MatchString(avatar) || len(avatar) > 700000:
			fail(w, http.StatusUnprocessableEntity, "Фото должно быть PNG, JPEG или WebP до 500 КБ.")
			return
		case !validAvatar(avatar):
			fail(w, http.StatusUnprocessableEntity, "Не удалось прочитать фото. Выберите корректный PNG, JPEG или WebP.")
			return
		default:
			user.Avatar = &avatar
		}
	}
	if err := s.store.UpdateUser(r.Context(), user); err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, user)
}

// analyzeJSON runs the analysis and serialises it for storage.
func analyzeJSON(content string) (analysis.Result, string, error) {
	result := analysis.Analyze(content)
	raw, err := json.Marshal(result)
	return result, string(raw), err
}
