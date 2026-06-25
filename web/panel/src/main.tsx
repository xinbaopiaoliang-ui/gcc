import React from "react";
import ReactDOM from "react-dom/client";
import { ConfigProvider } from "antd";
import zhCN from "antd/locale/zh_CN";
import App from "./App";
import "./styles.css";

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <ConfigProvider
      locale={zhCN}
      theme={{
        token: {
          colorPrimary: "#2563eb",
          colorInfo: "#2563eb",
          colorSuccess: "#0f766e",
          colorWarning: "#b45309",
          colorError: "#b42318",
          colorTextBase: "#172033",
          colorBgBase: "#f6f8fb",
          borderRadius: 6,
          fontFamily:
            "Geist, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, Segoe UI, sans-serif"
        },
        components: {
          Button: {
            controlHeight: 34,
            borderRadius: 6
          },
          Card: {
            borderRadiusLG: 6
          },
          Table: {
            headerBg: "#f8fafc",
            headerColor: "#46556f",
            rowHoverBg: "#f3f7ff"
          }
        }
      }}
    >
      <App />
    </ConfigProvider>
  </React.StrictMode>
);
