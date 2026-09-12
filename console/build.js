// Build script to assemble production dashboard assets into internal/dashboard/dist

import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { execSync } from "node:child_process";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

let esbuild;
try {
  esbuild = await import("esbuild");
} catch {
  execSync("npm install --no-fund --no-audit", { cwd: __dirname, stdio: "inherit" });
  esbuild = await import("esbuild");
}

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

// 4. Bundle JS files into a single standalone app.js using esbuild
esbuild.buildSync({
  entryPoints: ["app.js"],
  absWorkingDir: SRC_DIR,
  bundle: true,
  outfile: path.join(ASSETS_DIST, "app.js"),
  format: "iife",
});

// 5. Validate the output to ensure syntax is valid and no regression
try {
  execSync(`node --check ${path.join(ASSETS_DIST, "app.js")}`);
} catch (e) {
  console.error("Syntax validation failed!");
  process.exit(1);
}

console.log("Relay dashboard built successfully into internal/dashboard/dist/");
