import React from 'react';
import { createRoot } from 'react-dom/client';
import { ConfigProvider } from 'antd';
import ruRU from 'antd/locale/ru_RU';
import App from './App.jsx';
import 'antd/dist/reset.css';
import '@fontsource-variable/inter';
import './styles.css';

const theme = {
  token: {
    colorPrimary: '#4f46e5',
    colorLink: '#4f46e5',
    borderRadius: 10,
    fontSize: 14,
    fontFamily: '"Inter Variable", Inter, -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Arial, sans-serif',
    colorBgLayout: '#f8fafc',
    colorText: '#0f172a',
    colorTextSecondary: '#64748b',
    colorBorder: '#e2e8f0',
    colorBorderSecondary: '#eef2f7',
    controlHeight: 38,
  },
};

createRoot(document.getElementById('root')).render(
  <React.StrictMode>
    <ConfigProvider locale={ruRU} theme={theme}>
      <App />
    </ConfigProvider>
  </React.StrictMode>,
);
