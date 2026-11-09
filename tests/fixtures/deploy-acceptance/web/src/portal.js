const status = document.querySelector("#readiness");
async function refresh() {
  status.textContent = "Checking readiness…";
  try {
    const response = await fetch("/health/ready", { cache: "no-store" });
    const report = await response.json();
    status.textContent = response.ok && report.ready === true ? "Ready" : "Not ready";
  } catch {
    status.textContent = "Readiness unavailable";
  }
}
document.querySelector("#refresh").addEventListener("click", refresh);
refresh();
