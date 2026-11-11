// Keep private data tied to both the authenticated session and the open document.
// Every async consumer captures these generations before it sends its request.
export function createRequestGuard() {
  const sessionEpoch = { current: 0 };
  const documentEpoch = { current: 0 };
  const listEpoch = { current: 0 };
  const sessionValid = epoch => epoch === sessionEpoch.current;
  const documentValid = (epoch, request) =>
    sessionValid(epoch) && request === documentEpoch.current;

  async function runDocument(epoch, request, load, apply) {
    const result = await load();
    if (!documentValid(epoch, request)) return false;
    apply(result);
    return true;
  }

  return { sessionEpoch, documentEpoch, listEpoch, sessionValid, documentValid, runDocument };
}
