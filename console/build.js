// Build script to assemble production dashboard assets into internal/dashboard/dist

import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const SRC_DIR = path.join(__dirname, "src");
const DIST_DIR = path.join(__dirname, "..", "internal", "dashboard", "dist");
const ASSETS_DIST = path.join(DIST_DIR, "assets");

// Ensure directories exist
fs.mkdirSync(DIST_DIR, { recursive: true });
fs.mkdirSync(ASSETS_DIST, { recursive: true });

// 1. Copy index.html
const indexHtml = fs.readFileSync(path.join(SRC_DIR, "index.html"), "utf-8");
fs.writeFileSync(path.join(DIST_DIR, "index.html"), indexHtml.trim() + "\n");

// 2. Copy CSS
const appCss = fs.readFileSync(path.join(SRC_DIR, "app.css"), "utf-8");
fs.writeFileSync(path.join(ASSETS_DIST, "app.css"), appCss.trim() + "\n");

// 3. Copy Assets (favicon, etc.)
if (fs.existsSync(path.join(SRC_DIR, "assets", "favicon.svg"))) {
  fs.copyFileSync(
    path.join(SRC_DIR, "assets", "favicon.svg"),
    path.join(ASSETS_DIST, "favicon.svg")
  );
}

// 4. Bundle JS files into a single standalone app.js
// Read components
const statusPillJs = fs.readFileSync(path.join(SRC_DIR, "lib", "components", "StatusPill.js"), "utf-8")
  .replace(/export\s+/g, "");

const jsonViewerJs = fs.readFileSync(path.join(SRC_DIR, "lib", "components", "JsonViewer.js"), "utf-8")
  .replace(/export\s+/g, "");

const workflowDagJs = fs.readFileSync(path.join(SRC_DIR, "lib", "components", "WorkflowDAG.js"), "utf-8")
  .replace(/export\s+/g, "");

// Read API client
const clientJs = fs.readFileSync(path.join(SRC_DIR, "lib", "api", "client.ts"), "utf-8")
  // Strip typescript imports and type annotations for pure JS runtime
  .replace(/import\s+type\s+[^;]+;/g, "")
  .replace(/:\s*string\s*\|\s*null/g, "")
  .replace(/:\s*string/g, "")
  .replace(/:\s*number/g, "")
  .replace(/:\s*boolean/g, "")
  .replace(/:\s*void/g, "")
  .replace(/:\s*RequestInit/g, "")
  .replace(/:\s*WorkflowSearchQuery/g, "")
  .replace(/:\s*CreateAlertInput/g, "")
  .replace(/:\s*string\[\]/g, "")
  .replace(/:\s*Record<string,\s*unknown>/g, "")
  .replace(/<[^>]+>/g, "")
  .replace(/export\s+/g, "");

// Read app.js
const appJs = fs.readFileSync(path.join(SRC_DIR, "app.js"), "utf-8")
  .replace(/import\s+[^;]+;/g, "");

const bundledJs = `// Relay Dashboard Production Bundle
(function() {
  "use strict";

  // --- API Client ---
  ${clientJs}

  // --- Components ---
  ${statusPillJs}
  ${jsonViewerJs}
  ${workflowDagJs}

  // --- Application ---
  ${appJs}
})();
`;

fs.writeFileSync(path.join(ASSETS_DIST, "app.js"), bundledJs.trim() + "\n");

console.log("Relay dashboard built successfully into internal/dashboard/dist/");
