
import { readFileSync } from "node:fs";
import { Agent } from "node:https";
import { fileURLToPath } from "node:url";

import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

import {
  defineConfig,
  type Plugin,
  type ProxyOptions,
} from "vite";

const DEV_HOST = "127.0.0.1";
const DEV_PORT = 5173;

const DEV_AUTHORITY = `${DEV_HOST}:${DEV_PORT}`;
const DEV_ORIGIN = `http://${DEV_AUTHORITY}`;

const API_ROUTE =
  "^/(?:api(?:/|\\?|$)|(?:healthz|readyz)(?:\\?|$))";

const apiRoutePattern = new RegExp(API_ROUTE);

function readAPIPort(): number {
  const value =
    process.env.RACKOS_DEV_API_PORT ?? "8080";

  if (!/^[1-9][0-9]{0,4}$/.test(value)) {
    throw new Error(
      "RACKOS_DEV_API_PORT must be a numeric port",
    );
  }

  const port = Number(value);

  if (
    !Number.isInteger(port) ||
    port > 65535 ||
    port === DEV_PORT
  ) {
    throw new Error(
      "Invalid RackOS development API port",
    );
  }

  return port;
}

function localDevelopmentSecurity(): Plugin {
  return {
    name: "rackos-local-development-security",

    configureServer(server) {
      server.middlewares.use((req, res, next) => {
        const hostHeaders = req.rawHeaders.filter(
          (value, index) =>
            index % 2 === 0 &&
            value.toLowerCase() === "host",
        );

        const origin = req.headers.origin;

        const fetchSite =
          req.headers["sec-fetch-site"];

        const pathname = req.url ?? "";

        const validAuthority =
          hostHeaders.length === 1 &&
          req.headers.host === DEV_AUTHORITY;

        const validOrigin =
          origin === undefined ||
          origin === DEV_ORIGIN;

        const validFetchSite =
          fetchSite === undefined ||
          fetchSite === "same-origin" ||
          fetchSite === "none";

        const validPath =
          pathname.startsWith("/") &&
          !pathname.startsWith("//") &&
          !pathname.includes("\\");

        res.setHeader(
          "Cache-Control",
          "no-store",
        );

        res.setHeader(
          "X-Content-Type-Options",
          "nosniff",
        );

        res.setHeader(
          "X-Frame-Options",
          "DENY",
        );

        res.setHeader(
          "Referrer-Policy",
          "no-referrer",
        );

        res.setHeader(
          "Cross-Origin-Resource-Policy",
          "same-origin",
        );

        if (
          !validAuthority ||
          !validOrigin ||
          !validFetchSite ||
          !validPath
        ) {
          res.statusCode = 403;

          res.setHeader(
            "Content-Type",
            "application/json; charset=utf-8",
          );

          res.end(
            JSON.stringify({
              error: "development request forbidden",
            }),
          );

          return;
        }

        if (
          apiRoutePattern.test(pathname) &&
          req.method !== "GET" &&
          req.method !== "HEAD"
        ) {
          res.statusCode = 405;

          res.setHeader("Allow", "GET, HEAD");

          res.setHeader(
            "Content-Type",
            "application/json; charset=utf-8",
          );

          res.end(
            JSON.stringify({
              error: "development API proxy is read-only",
            }),
          );

          return;
        }

        next();
      });
    },
  };
}

function createAPIProxy(): ProxyOptions {
  const port = readAPIPort();

  const caPath =
    process.env.RACKOS_DEV_API_CA;

  if (caPath !== undefined && caPath.trim() === "") {
    throw new Error(
      "RACKOS_DEV_API_CA cannot be empty",
    );
  }

  const tlsEnabled = caPath !== undefined;

  const scheme = tlsEnabled
    ? "https"
    : "http";

  const target = `${scheme}://${DEV_HOST}:${port}`;

  const agent = caPath
    ? new Agent({
        ca: readFileSync(caPath),
        rejectUnauthorized: true,
        minVersion: "TLSv1.3",
      })
    : undefined;

  return {
    target,
    changeOrigin: true,
    secure: true,
    ws: false,
    agent,

    configure(proxy) {
      proxy.on("proxyReq", (proxyReq, req) => {
        if (req.headers.origin === DEV_ORIGIN) {
          proxyReq.setHeader("Origin", target);
        }

        // This is a read-only development proxy,
        // not an administrator authentication channel.
        proxyReq.removeHeader("Cookie");
        proxyReq.removeHeader("Authorization");
        proxyReq.removeHeader("Proxy-Authorization");
      });

      proxy.on("proxyRes", (proxyRes) => {
        delete proxyRes.headers["set-cookie"];
      });
    },
  };
}

export default defineConfig(
  ({ command, isPreview }) => {
    const development =
      command === "serve" && !isPreview;

    return {
      plugins: [
        react(),
        tailwindcss(),
        localDevelopmentSecurity(),
      ],

      server: {
        host: DEV_HOST,
        port: DEV_PORT,
        strictPort: true,
        cors: false,

        fs: {
          strict: true,
          allow: [
            fileURLToPath(
              new URL("../../", import.meta.url),
            ),
          ],
        },

        proxy: development
          ? {
              [API_ROUTE]: createAPIProxy(),
            }
          : undefined,
      },

      preview: {
        host: DEV_HOST,
        port: 4173,
        strictPort: true,
        cors: false,
      },

      build: {
        emptyOutDir: true,
        sourcemap: false,
      },
    };
  },
);
