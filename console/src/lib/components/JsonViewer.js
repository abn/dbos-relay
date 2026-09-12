// JsonViewer component for formatted payload inspection

export function renderJsonViewer(data, title = "") {
  if (data === undefined || data === null || data === "") {
    return `<div class="json-viewer empty"><em>No data</em></div>`;
  }

  let formatted = "";
  let raw = "";

  if (typeof data === "string") {
    raw = data;
    try {
      const parsed = JSON.parse(data);
      formatted = syntaxHighlight(JSON.stringify(parsed, null, 2));
    } catch {
      formatted = `<span class="json-string">${escapeHtml(data)}</span>`;
    }
  } else {
    raw = JSON.stringify(data, null, 2);
    formatted = syntaxHighlight(raw);
  }

  const id = "json-" + Math.random().toString(36).substring(2, 9);

  return `
    <div class="json-viewer" id="${id}">
      <div class="json-header">
        <span class="json-title">${escapeHtml(title)}</span>
        <button class="btn btn-xs btn-secondary copy-btn" data-copy="${escapeHtml(raw)}">Copy</button>
      </div>
      <pre class="json-content"><code>${formatted}</code></pre>
    </div>
  `;
}

function syntaxHighlight(json) {
  const escaped = escapeHtml(json);
  return escaped.replace(
    /("(\\u[a-zA-Z0-9]{4}|\\[^u]|[^\\"])*"(\s*:)?|\b(true|false|null)\b|-?\d+(?:\.\d*)?(?:[eE][+\\-]?\d+)?)/g,
    (match) => {
      let cls = "json-number";
      if (/^"/.test(match)) {
        if (/:$/.test(match)) {
          cls = "json-key";
        } else {
          cls = "json-string";
        }
      } else if (/true|false/.test(match)) {
        cls = "json-boolean";
      } else if (/null/.test(match)) {
        cls = "json-null";
      }
      return `<span class="${cls}">${match}</span>`;
    }
  );
}

function escapeHtml(str) {
  return String(str)
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#039;");
}
