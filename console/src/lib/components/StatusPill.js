// StatusPill component rendering standard status badges

export function renderStatusPill(status) {
  const s = (status || "UNKNOWN").toUpperCase();
  let colorClass = "pill-neutral";

  switch (s) {
    case "SUCCESS":
    case "HEALTHY":
    case "AVAILABLE":
      colorClass = "pill-success";
      break;
    case "PENDING":
    case "ENQUEUED":
    case "RUNNING":
      colorClass = "pill-info";
      break;
    case "ERROR":
    case "DEAD":
    case "UNAVAILABLE":
    case "MAX_RECOVERY_ATTEMPTS_EXCEEDED":
      colorClass = "pill-error";
      break;
    case "CANCELLED":
    case "DISCONNECTED":
      colorClass = "pill-warning";
      break;
    case "DELAYED":
      colorClass = "pill-purple";
      break;
  }

  return `<span class="status-pill ${colorClass}"><span class="dot"></span>${escapeHtml(s)}</span>`;
}

function escapeHtml(str) {
  return String(str)
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#039;");
}
