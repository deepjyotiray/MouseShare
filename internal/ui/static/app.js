const stateEls = {
  pairCode: document.getElementById("pairCode"),
  deviceSummary: document.getElementById("deviceSummary"),
  permissions: document.getElementById("permissions"),
  peers: document.getElementById("peers"),
  transfers: document.getElementById("transfers"),
  layoutInput: document.getElementById("layoutInput"),
  peerSelect: document.getElementById("peerSelect"),
  manualPairStatus: document.getElementById("manualPairStatus"),
  sendStatus: document.getElementById("sendStatus"),
  layoutStatus: document.getElementById("layoutStatus"),
};

async function fetchState() {
  const response = await fetch("/api/state");
  const state = await response.json();
  render(state);
}

function render(state) {
  stateEls.pairCode.textContent = state.self.pairCode;
  stateEls.deviceSummary.innerHTML = `
    <p><strong>${state.self.name}</strong></p>
    <p>${state.self.os} · ${state.self.addr || "waiting for LAN address"}:${state.self.port}</p>
    <p>Fingerprint: <code>${state.self.fingerprint.slice(0, 18)}...</code></p>
  `;

  stateEls.permissions.innerHTML = "";
  const permissions = [
    ["Accessibility", state.permissions.accessibility],
    ["Input capture", state.permissions.inputCapture],
    ["Screen access", state.permissions.screenAccess],
  ];
  permissions.forEach(([label, ok]) => {
    const li = document.createElement("li");
    li.textContent = `${label}: ${ok ? "ready" : "setup needed"}`;
    stateEls.permissions.appendChild(li);
  });
  (state.permissions.warnings || []).forEach((warning) => {
    const li = document.createElement("li");
    li.textContent = warning;
    stateEls.permissions.appendChild(li);
  });

  stateEls.peers.innerHTML = "";
  stateEls.peerSelect.innerHTML = `<option value="">Choose peer</option>`;
  state.peers.forEach((peer) => {
    const card = document.createElement("article");
    card.className = "peer";
    const lastSeen = new Date(peer.device.seenAt).toLocaleTimeString();
    card.innerHTML = `
      <header>
        <strong>${peer.device.name}</strong>
        <span class="badge">${peer.status}</span>
      </header>
      <p>${peer.device.os} · ${peer.device.addr}:${peer.device.port}</p>
      <p>Last seen ${lastSeen}</p>
      <div class="row">
        <button data-action="approve" data-peer="${peer.device.id}">Approve</button>
        <button data-action="reject" data-peer="${peer.device.id}">Reject</button>
        ${peer.status === "trusted" ? `<button data-action="control-start" data-peer="${peer.device.id}">Start control</button>` : ""}
        ${state.control && state.control.mode === "controlling" && state.control.activePeerId === peer.device.id ? `<button data-action="control-stop" data-peer="${peer.device.id}">Stop control</button>` : ""}
      </div>
    `;
    stateEls.peers.appendChild(card);
    if (peer.status === "trusted") {
      const option = document.createElement("option");
      option.value = peer.device.id;
      option.textContent = `${peer.device.name} (${peer.device.os})`;
      stateEls.peerSelect.appendChild(option);
    }
  });

  stateEls.transfers.innerHTML = state.transfers.map((job) => `
    <article class="peer">
      <header>
        <strong>${job.fileName}</strong>
        <span class="badge">${job.status}</span>
      </header>
      <p>${job.direction} · ${job.bytesDone} / ${job.bytesTotal} bytes</p>
      ${job.error ? `<p>${job.error}</p>` : ""}
      ${job.downloadDir ? `<p>${job.downloadDir}</p>` : ""}
    </article>
  `).join("") || `<p class="caption">No transfers yet.</p>`;

  stateEls.layoutInput.value = JSON.stringify(state.layout, null, 2);
}

async function postJSON(url, payload) {
  const response = await fetch(url, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  if (!response.ok) {
    throw new Error(await response.text());
  }
}

document.getElementById("refreshBtn").addEventListener("click", fetchState);

document.getElementById("peers").addEventListener("click", async (event) => {
  const button = event.target.closest("button[data-action]");
  if (!button) return;
  const action = button.dataset.action;
  const peerId = button.dataset.peer;
  try {
    if (action === "control-start") {
      await postJSON("/api/control/start", { peerId });
    } else if (action === "control-stop") {
      await postJSON("/api/control/stop", {});
    } else {
      await postJSON(`/api/${action}`, { peerId });
    }
    fetchState();
  } catch (error) {
    alert(error.message);
  }
});

document.getElementById("manualPairForm").addEventListener("submit", async (event) => {
  event.preventDefault();
  const form = new FormData(event.target);
  try {
    await postJSON("/api/manual-pair", {
      addr: form.get("addr"),
      code: form.get("code"),
    });
    stateEls.manualPairStatus.textContent = "Peer trusted.";
    fetchState();
  } catch (error) {
    stateEls.manualPairStatus.textContent = error.message;
  }
});

document.getElementById("saveLayoutBtn").addEventListener("click", async () => {
  try {
    const layout = JSON.parse(stateEls.layoutInput.value);
    await postJSON("/api/layout", layout);
    stateEls.layoutStatus.textContent = "Layout saved.";
  } catch (error) {
    stateEls.layoutStatus.textContent = error.message;
  }
});

document.getElementById("sendForm").addEventListener("submit", async (event) => {
  event.preventDefault();
  const peerId = stateEls.peerSelect.value;
  if (!peerId) {
    stateEls.sendStatus.textContent = "Choose a trusted peer first.";
    return;
  }
  const payload = new FormData();
  for (const file of document.getElementById("filesInput").files) {
    payload.append("files", file, file.webkitRelativePath || file.name);
  }
  const response = await fetch(`/api/send?peerId=${encodeURIComponent(peerId)}`, {
    method: "POST",
    body: payload,
  });
  if (!response.ok) {
    stateEls.sendStatus.textContent = await response.text();
    return;
  }
  stateEls.sendStatus.textContent = "Transfer started.";
  fetchState();
});

fetchState();
setInterval(fetchState, 2500);
