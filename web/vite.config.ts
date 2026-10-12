import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// En desarrollo la API se alcanza por proxy para mantener el mismo origen:
// el servidor no envía cabeceras CORS (ver docs/DECISIONES.md, D-013).
const api = "http://127.0.0.1:8080";

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      "/api": api,
      "/healthz": api,
    },
  },
});
