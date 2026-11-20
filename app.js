const byId = (id) => document.getElementById(id);
const summaries = {
  positive:
    "The code requests Basil and calls retrieveUpcoming. If this project targets Stripe Node v18, review the SDK migration at line 3.",
  compatible:
    "Line 3 calls createPreview. Both pinned SDK versions include this method. The same scan rule returned zero migration findings for this caller.",
  direct_http:
    "This request sets the Basil header and calls GET /v1/invoices/upcoming. The pinned Basil contract omits this endpoint. Review line 2 before deployment.",
};
const textEncoder = new TextEncoder();
async function digest(bytes) {
  const result = await crypto.subtle.digest("SHA-256", bytes);
  return Array.from(new Uint8Array(result), (byte) =>
    byte.toString(16).padStart(2, "0"),
  ).join("");
}
async function readFile(path) {
  const response = await fetch(path);
  if (!response.ok) throw new Error("Could not load " + path);
  return response;
}

async function openReport() {
  const report = await (await readFile("evidence/reports.json")).json();
  const catalogBytes = await (
    await readFile("catalog/stripe-invoice-v1.json")
  ).arrayBuffer();
  const catalog = JSON.parse(new TextDecoder().decode(catalogBytes));
  let active;
  const setText = (id, val) => {
    byId(id).textContent = val;
  };
  function select(item) {
    active = item;
    for (const btn of byId("choices").children) {
      const selected = btn.dataset.key === item.key;
      btn.setAttribute("aria-pressed", String(selected));
      btn.lastElementChild.textContent = selected ? "↗" : "";
    }
    setText("title", item.label);
    setText(
      "lang",
      item.language === "typescript"
        ? "TypeScript · Stripe SDK"
        : "Python · Direct HTTP",
    );
    setText("status", item.findingCount === 1 ? "1 finding" : "0 findings");
    byId("status").classList.toggle("clean", item.findingCount === 0);
    setText("filepath", item.path);
    setText("lineNo", "Line " + item.line);
    byId("code").replaceChildren();
    for (const [index, line] of item.sourceText
      .trimEnd()
      .split("\n")
      .entries()) {
      const row = document.createElement("span");
      row.textContent = line;
      row.dataset.line = index + 1;
      if (index + 1 === item.line) row.className = "hit";
      byId("code").append(row);
    }
    setText("meaning", summaries[item.key]);
    setText("version", item.apiVersion);
    setText("unknowns", item.missing.join(", "));
    setText(
      "raw-finding",
      item.review?.summary ||
        "The exported report contains an empty findings array and one resolved usage.",
    );
    byId("details").open = false;
    setText("verified", "");
  }
  for (const item of report.cases) {
    const btn = document.createElement("button");
    btn.type = "button";
    btn.className = "choice";
    btn.dataset.key = item.key;
    const tag = document.createElement("span");
    tag.className = "tag";
    tag.textContent = item.language === "typescript" ? "TS" : "PY";
    const title = document.createElement("strong");
    title.textContent = item.label;
    const arrow = document.createElement("span");
    arrow.className = "arrow";
    arrow.ariaHidden = "true";
    btn.append(tag, title, arrow);
    btn.addEventListener("click", () => select(item));
    byId("choices").append(btn);
  }
  byId("verify").addEventListener("click", async () => {
    const item = active;
    try {
      const blob = await digest(textEncoder.encode(item.sourceText));
      const registry = await digest(catalogBytes);
      const [before, after] = catalog.snapshots;
      const beforeGet = before.operations.find(
        (op) => op.method === "GET" && op.path === "/v1/invoices/upcoming",
      );
      const afterGet = after.operations.find(
        (op) => op.method === "GET" && op.path === "/v1/invoices/upcoming",
      );
      const matches =
        registry === report.catalogSha256 &&
        blob === item.sourceSha256 &&
        item.sourceText.split("\n")[item.line - 1]?.includes(item.caller) &&
        beforeGet?.present === true &&
        afterGet?.present === false;
      if (active === item)
        setText(
          "verified",
          matches
            ? "✓ Source hash, cited line and contract match."
            : "Evidence mismatch. Download a fresh copy and retry.",
        );
    } catch {
      if (active === item)
        setText(
          "verified",
          "Open this report over HTTPS or localhost to verify its hashes.",
        );
    }
  });
  select(report.cases[0]);
  byId("loading").hidden = true;
  byId("lab").hidden = false;
}
openReport().catch(() => {
  byId("loading").textContent =
    "Could not load the report. Serve this folder over HTTP and reload.";
});
