const stateEls = {
  pairCode: document.getElementById("pairCode"),
  deviceSummary: document.getElementById("deviceSummary"),
  permissions: document.getElementById("permissions"),
  peers: document.getElementById("peers"),
  transfers: document.getElementById("transfers"),
  layoutInput: document.getElementById("layoutInput"),
  layoutCanvas: document.getElementById("layoutCanvas"),
  layoutControls: document.getElementById("layoutControls"),
  peerSelect: document.getElementById("peerSelect"),
  manualPairStatus: document.getElementById("manualPairStatus"),
  sendStatus: document.getElementById("sendStatus"),
  layoutStatus: document.getElementById("layoutStatus"),
};

let latestState = null;
let layoutDraft = [];

async function fetchState() {
  const response = await fetch("/api/state");
  const state = await response.json();
  latestState = state;
  syncLayoutDraft(state);
  render(state);
}

function syncLayoutDraft(state) {
  const byId = new Map((state.layout || []).map((node) => [node.deviceId, { ...node }]));
  const allDevices = [state.self, ...state.peers.map((peer) => peer.device)];

  if (layoutDraft.length === 0) {
    layoutDraft = [];
  }

  for (const device of allDevices) {
    if (!layoutDraft.find((node) => node.deviceId === device.id)) {
      const existing = byId.get(device.id);
      layoutDraft.push(existing || defaultNodeFor(device, state.self.id));
    }
  }

  layoutDraft = layoutDraft
    .filter((node) => allDevices.some((device) => device.id === node.deviceId))
    .map((node) => {
      const existing = byId.get(node.deviceId);
      if (existing) {
        return {
          ...node,
          width: existing.width || node.width,
          height: existing.height || node.height,
          x: existing.x,
          y: existing.y,
        };
      }
      return node;
    });

  ensureSelfAtOrigin(state.self.id);
}

function defaultNodeFor(device, selfId) {
  return {
    deviceId: device.id,
    x: device.id === selfId ? 0 : 1,
    y: 0,
    width: device.screenWidth || 1440,
    height: device.screenHeight || 900,
  };
}

function ensureSelfAtOrigin(selfId) {
  const selfNode = layoutDraft.find((node) => node.deviceId === selfId);
  if (!selfNode) return;
  const offsetX = selfNode.x;
  const offsetY = selfNode.y;
  layoutDraft = layoutDraft.map((node) => ({
    ...node,
    x: node.x - offsetX,
    y: node.y - offsetY,
  }));
}

function render(state) {
  stateEls.pairCode.textContent = state.self.pairCode;
  stateEls.deviceSummary.innerHTML = `
    <p><strong>${state.self.name}</strong></p>
    <p>${state.self.os} · ${state.self.addr || "waiting for LAN address"}:${state.self.port}</p>
    <p>Screen: ${state.self.screenWidth || "?"} × ${state.self.screenHeight || "?"}</p>
    <p>Fingerprint: <code>${state.self.fingerprint.slice(0, 18)}...</code></p>
  `;

  renderPermissions(state);
  renderPeers(state);
  renderTransfers(state);
  renderLayout(state);
}

function renderPermissions(state) {
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
}

function renderPeers(state) {
  stateEls.peers.innerHTML = "";
  stateEls.peerSelect.innerHTML = `<option value="">Choose peer</option>`;
  state.peers.forEach((peer) => {
    const node = layoutDraft.find((item) => item.deviceId === peer.device.id);
    const card = document.createElement("article");
    card.className = "peer";
    const lastSeen = new Date(peer.device.seenAt).toLocaleTimeString();
    card.innerHTML = `
      <header>
        <strong>${peer.device.name}</strong>
        <span class="badge">${peer.status}</span>
      </header>
      <p>${peer.device.os} · ${peer.device.addr}:${peer.device.port}</p>
      <p>Screen ${peer.device.screenWidth || "?"} × ${peer.device.screenHeight || "?"}</p>
      <p>Last seen ${lastSeen}</p>
      <p>${node ? `Layout (${node.x}, ${node.y})` : "No layout yet"}</p>
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
}

function renderTransfers(state) {
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
}

function renderLayout(state) {
  stateEls.layoutInput.value = JSON.stringify(layoutDraft, null, 2);
  renderLayoutCanvas(state);
  renderLayoutControls(state);
}

function renderLayoutCanvas(state) {
  const bounds = computeCanvasBounds(layoutDraft);
  const scaleX = 520 / Math.max(bounds.width, 1);
  const scaleY = 280 / Math.max(bounds.height, 1);
  const scale = Math.min(scaleX, scaleY, 0.22);
  stateEls.layoutCanvas.innerHTML = "";

  layoutDraft.forEach((node) => {
    const device = findDevice(state, node.deviceId);
    if (!device) return;
    const tile = document.createElement("div");
    tile.className = `layout-tile ${node.deviceId === state.self.id ? "self" : "peer"}`;
    tile.style.left = `${18 + (node.x - bounds.minX) * scale}px`;
    tile.style.top = `${18 + (node.y - bounds.minY) * scale}px`;
    tile.style.width = `${Math.max(90, node.width * scale)}px`;
    tile.style.height = `${Math.max(70, node.height * scale)}px`;
    tile.innerHTML = `
      <strong>${device.name}</strong>
      <div class="layout-meta">
        <div>${device.os}</div>
        <div>${node.width} × ${node.height}</div>
        <div>x ${node.x}, y ${node.y}</div>
      </div>
    `;
    stateEls.layoutCanvas.appendChild(tile);
  });
}

function renderLayoutControls(state) {
  stateEls.layoutControls.innerHTML = "";
  const selfNode = layoutDraft.find((node) => node.deviceId === state.self.id);
  const selfDevice = state.self;

  const selfCard = document.createElement("article");
  selfCard.className = "layout-device";
  selfCard.innerHTML = `
    <strong>${selfDevice.name}</strong>
    <p class="caption">${selfDevice.os} · anchor device</p>
    <p class="caption">Fixed at (${selfNode?.x ?? 0}, ${selfNode?.y ?? 0})</p>
  `;
  stateEls.layoutControls.appendChild(selfCard);

  state.peers.forEach((peer) => {
    const node = layoutDraft.find((item) => item.deviceId === peer.device.id) || defaultNodeFor(peer.device, state.self.id);
    const card = document.createElement("article");
    card.className = "layout-device";
    card.innerHTML = `
      <strong>${peer.device.name}</strong>
      <p class="caption">${peer.device.os} · ${peer.status}</p>
      <p class="caption">Current: (${node.x}, ${node.y}) · ${node.width} × ${node.height}</p>
      <div class="row">
        <button data-layout-place="left" data-peer="${peer.device.id}">Left of this Mac/PC</button>
        <button data-layout-place="right" data-peer="${peer.device.id}">Right</button>
        <button data-layout-place="up" data-peer="${peer.device.id}">Above</button>
        <button data-layout-place="down" data-peer="${peer.device.id}">Below</button>
      </div>
    `;
    stateEls.layoutControls.appendChild(card);
  });
}

function computeCanvasBounds(layout) {
  if (!layout.length) {
    return { minX: 0, minY: 0, width: 1, height: 1 };
  }
  const minX = Math.min(...layout.map((node) => node.x));
  const minY = Math.min(...layout.map((node) => node.y));
  const maxX = Math.max(...layout.map((node) => node.x + node.width));
  const maxY = Math.max(...layout.map((node) => node.y + node.height));
  return { minX, minY, width: maxX - minX, height: maxY - minY };
}

function findDevice(state, deviceId) {
  if (state.self.id === deviceId) return state.self;
  return state.peers.find((peer) => peer.device.id === deviceId)?.device;
}

function placePeer(peerId, direction) {
  if (!latestState) return;
  const selfNode = layoutDraft.find((node) => node.deviceId === latestState.self.id);
  const peerDevice = findDevice(latestState, peerId);
  if (!selfNode || !peerDevice) return;
  const peerNode = layoutDraft.find((node) => node.deviceId === peerId) || defaultNodeFor(peerDevice, latestState.self.id);
  const next = { ...peerNode, width: peerDevice.screenWidth || peerNode.width, height: peerDevice.screenHeight || peerNode.height };

  switch (direction) {
    case "left":
      next.x = selfNode.x - next.width;
      next.y = selfNode.y;
      break;
    case "right":
      next.x = selfNode.x + selfNode.width;
      next.y = selfNode.y;
      break;
    case "up":
      next.x = selfNode.x;
      next.y = selfNode.y - next.height;
      break;
    case "down":
      next.x = selfNode.x;
      next.y = selfNode.y + selfNode.height;
      break;
  }

  layoutDraft = layoutDraft
    .filter((node) => node.deviceId !== peerId)
    .concat(next);
  ensureSelfAtOrigin(latestState.self.id);
  render(latestState);
  stateEls.layoutStatus.textContent = `Placed ${peerDevice.name} ${direction} of ${latestState.self.name}. Save layout to apply.`;
}

function autoArrange() {
  if (!latestState) return;
  const selfNode = layoutDraft.find((node) => node.deviceId === latestState.self.id) || defaultNodeFor(latestState.self, latestState.self.id);
  const peers = latestState.peers.map((peer) => peer.device);
  const next = [{ ...selfNode, x: 0, y: 0 }];
  let cursorX = selfNode.width;
  for (const peer of peers) {
    next.push({
      deviceId: peer.id,
      x: cursorX,
      y: 0,
      width: peer.screenWidth || 1440,
      height: peer.screenHeight || 900,
    });
    cursorX += peer.screenWidth || 1440;
  }
  layoutDraft = next;
  render(latestState);
  stateEls.layoutStatus.textContent = "Auto-arranged peers to the right of the local machine. Save layout to apply.";
}

async function saveLayoutDraft() {
  try {
    await postJSON("/api/layout", layoutDraft);
    stateEls.layoutStatus.textContent = "Layout saved.";
    await fetchState();
  } catch (error) {
    stateEls.layoutStatus.textContent = error.message;
  }
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
      await fetchState();
      return;
    }
    await fetchState();
  } catch (error) {
    alert(error.message);
  }
});

document.getElementById("layoutControls").addEventListener("click", (event) => {
  const button = event.target.closest("button[data-layout-place]");
  if (!button) return;
  placePeer(button.dataset.peer, button.dataset.layoutPlace);
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
    await fetchState();
  } catch (error) {
    stateEls.manualPairStatus.textContent = error.message;
  }
});

document.getElementById("saveLayoutBtn").addEventListener("click", saveLayoutDraft);
document.getElementById("autoLayoutBtn").addEventListener("click", autoArrange);

document.getElementById("layoutInput").addEventListener("change", () => {
  try {
    const parsed = JSON.parse(stateEls.layoutInput.value);
    if (Array.isArray(parsed)) {
      layoutDraft = parsed;
      ensureSelfAtOrigin(latestState.self.id);
      render(latestState);
    }
  } catch (_) {
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
  await fetchState();
});

fetchState();
setInterval(fetchState, 2500);
