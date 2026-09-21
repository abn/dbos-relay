// Build script to assemble production dashboard assets into internal/dashboard/dist

import crypto from "node:crypto";
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

// Fingerprinted file name: app.<12 hex sha256>.js. New content gets a new
// URL, so the immutable cache headers the server sends for assets/ stay
// correct across upgrades. Only the built JS and CSS are fingerprinted;
// static images keep stable names (the bundle references logo.svg at
// runtime, so renaming it would require rewriting bundle contents).
function fingerprintedName(base, ext, content) {
  const hash = crypto.createHash("sha256").update(content).digest("hex").slice(0, 12);
  return `${base}.${hash}.${ext}`;
}

// Remove stale fingerprinted outputs (and the legacy unhashed names) so
// the embedded dist never carries more than one JS/CSS generation.
function cleanStaleBundles(dir, base) {
  const stale = new RegExp(`^${base}\\.[0-9a-f]{12}\\.(js|css)$`);
  for (const file of fs.readdirSync(dir)) {
    if (file === `${base}.js` || file === `${base}.css` || stale.test(file)) {
      fs.rmSync(path.join(dir, file));
    }
  }
}

// Ensure directories exist
fs.mkdirSync(DIST_DIR, { recursive: true });
fs.mkdirSync(ASSETS_DIST, { recursive: true });

// 1. Bundle JS files into a single standalone bundle using esbuild
const bundle = esbuild.buildSync({
  entryPoints: ["app.js"],
  absWorkingDir: SRC_DIR,
  bundle: true,
  write: false,
  format: "iife",
});
const jsContent = bundle.outputFiles[0].contents;
const jsName = fingerprintedName("app", "js", jsContent);

// 2. Fingerprint CSS
const appCss = fs.readFileSync(path.join(SRC_DIR, "app.css"), "utf-8").trim() + "\n";
const cssName = fingerprintedName("app", "css", appCss);

// 3. Drop replaced bundles, write the new ones, rewrite index.html refs
cleanStaleBundles(ASSETS_DIST, "app");
fs.writeFileSync(path.join(ASSETS_DIST, jsName), jsContent);
fs.writeFileSync(path.join(ASSETS_DIST, cssName), appCss);
let indexHtml = fs.readFileSync(path.join(SRC_DIR, "index.html"), "utf-8");
indexHtml = indexHtml.replace(/assets\/app\.css/g, `assets/${cssName}`).replace(/assets\/app\.js/g, `assets/${jsName}`);
fs.writeFileSync(path.join(DIST_DIR, "index.html"), indexHtml.trim() + "\n");

// 4. Copy static assets (favicon, logo, etc.)
const srcAssetsDir = path.join(SRC_DIR, "assets");
if (fs.existsSync(srcAssetsDir)) {
  for (const file of fs.readdirSync(srcAssetsDir)) {
    fs.copyFileSync(path.join(srcAssetsDir, file), path.join(ASSETS_DIST, file));
  }
}

// 5. Validate the output to ensure syntax is valid and no regression
try {
  execSync(`node --check ${path.join(ASSETS_DIST, jsName)}`);
} catch (e) {
  console.error("Syntax validation failed!");
  process.exit(1);
}

// 6. Sync to site/playground/assets if directory exists
const PLAYGROUND_ASSETS = path.join(__dirname, "..", "site", "playground", "assets");
if (fs.existsSync(PLAYGROUND_ASSETS)) {
  cleanStaleBundles(PLAYGROUND_ASSETS, "app");
  fs.copyFileSync(path.join(ASSETS_DIST, jsName), path.join(PLAYGROUND_ASSETS, jsName));
  fs.copyFileSync(path.join(ASSETS_DIST, cssName), path.join(PLAYGROUND_ASSETS, cssName));
  const consoleHtmlPath = path.join(__dirname, "..", "site", "playground", "console.html");
  let consoleHtml = fs.readFileSync(consoleHtmlPath, "utf-8");
  consoleHtml = consoleHtml
    .replace(/assets\/app\.[0-9a-f]{12}\.js/g, `assets/${jsName}`)
    .replace(/assets\/app\.[0-9a-f]{12}\.css/g, `assets/${cssName}`)
    .replace(/assets\/app\.js/g, `assets/${jsName}`)
    .replace(/assets\/app\.css/g, `assets/${cssName}`);
  fs.writeFileSync(consoleHtmlPath, consoleHtml);
}

console.log(`Relay dashboard built successfully into internal/dashboard/dist/ (${jsName}, ${cssName})`);
