// Frontend controller for the howard web UI. Talks to the Go server over JSON
// + Server-Sent Events and reuses the same ChatResponse contract as the CLI/TUI.
(function () {
  "use strict";

  const log = document.getElementById("chat-log");
  const promptEl = document.getElementById("prompt");
  const sendBtn = document.getElementById("send");
  const stopBtn = document.getElementById("stop");
  const newChatBtn = document.getElementById("new-chat");
  const modelSelect = document.getElementById("model-select");
  const thinkingPanel = document.getElementById("thinking-panel");
  const thinkingContent = document.getElementById("thinking-content");
  const thinkingToggle = document.getElementById("thinking-toggle");

  const JSON_HEADERS = { "Content-Type": "application/json" };

  let responding = false;
  let currentModel = "";
  let assistantBuf = "";
  let assistantBubble = null;
  let thinkingBuf = "";

  function scrollToBottom() {
    log.scrollTop = log.scrollHeight;
  }

  function addMessage(role, label, opts) {
    const wrap = document.createElement("div");
    wrap.className = "msg " + role;

    const roleEl = document.createElement("div");
    roleEl.className = "role";
    roleEl.textContent = label;

    const bubble = document.createElement("div");
    bubble.className = "bubble";
    if (opts && opts.html != null) bubble.innerHTML = opts.html;
    else bubble.textContent = (opts && opts.text) || "";

    wrap.appendChild(roleEl);
    wrap.appendChild(bubble);
    log.appendChild(wrap);
    scrollToBottom();
    return bubble;
  }

  function ensureAssistantBubble() {
    if (!assistantBubble) {
      assistantBubble = addMessage("ai", currentModel || "howard", {
        html: "",
      });
      assistantBubble.classList.add("cursor");
    }
  }

  function renderAssistant() {
    ensureAssistantBubble();
    assistantBubble.innerHTML = window.renderMarkdown(assistantBuf);
    scrollToBottom();
  }

  function finalizeAssistant() {
    if (assistantBubble) {
      assistantBubble.classList.remove("cursor");
      // Drop an assistant bubble that never received content.
      if (!assistantBuf.trim()) assistantBubble.parentElement.remove();
    }
    assistantBubble = null;
    assistantBuf = "";
  }

  function updateThinking() {
    thinkingPanel.classList.remove("hidden");
    thinkingContent.textContent = thinkingBuf;
    thinkingContent.scrollTop = thinkingContent.scrollHeight;
  }

  function clearThinking() {
    thinkingBuf = "";
    thinkingContent.textContent = "";
    thinkingPanel.classList.add("hidden");
  }

  thinkingToggle.addEventListener("click", function () {
    thinkingPanel.classList.toggle("collapsed");
    const expanded = !thinkingPanel.classList.contains("collapsed");
    thinkingToggle.setAttribute("aria-expanded", String(expanded));
  });

  function setResponding(v) {
    responding = v;
    promptEl.disabled = v;
    sendBtn.classList.toggle("hidden", v);
    stopBtn.classList.toggle("hidden", !v);
    modelSelect.disabled = v;
    newChatBtn.disabled = v;
    if (!v) promptEl.focus();
  }

  function handleMessage(msg) {
    if (msg.kind === "error") {
      finalizeAssistant();
      addMessage("error", "error", { text: msg.response || "unknown error" });
      return;
    }
    if (msg.kind === "tool_status") {
      const t = (msg.response || "").trim();
      if (t) addMessage("tool", "tool", { text: t });
      return;
    }
    if (msg.thinking) {
      thinkingBuf += msg.thinking;
      updateThinking();
    }
    if (msg.response) {
      assistantBuf += msg.response;
      renderAssistant();
    }
  }

  async function safeText(resp) {
    try {
      return (await resp.text()).trim();
    } catch (_) {
      return "";
    }
  }

  async function sendPrompt(text) {
    addMessage("user", "you", { text: text });
    assistantBuf = "";
    assistantBubble = null;
    clearThinking();
    setResponding(true);

    try {
      const resp = await fetch("/api/chat", {
        method: "POST",
        headers: JSON_HEADERS,
        body: JSON.stringify({ prompt: text }),
      });

      if (!resp.ok || !resp.body) {
        const errText = await safeText(resp);
        addMessage("error", "error", {
          text: errText || "HTTP " + resp.status,
        });
        return;
      }

      const reader = resp.body.getReader();
      const decoder = new TextDecoder();
      let buf = "";
      let finished = false;

      while (!finished) {
        const { value, done } = await reader.read();
        if (done) break;
        buf += decoder.decode(value, { stream: true });

        let idx;
        while ((idx = buf.indexOf("\n\n")) >= 0) {
          const raw = buf.slice(0, idx);
          buf = buf.slice(idx + 2);
          const line = raw.replace(/^data:\s?/, "");
          if (!line) continue;

          let msg;
          try {
            msg = JSON.parse(line);
          } catch (_) {
            continue;
          }
          handleMessage(msg);
          if (msg.done) {
            finished = true;
            break;
          }
        }
      }
    } catch (e) {
      addMessage("error", "error", { text: String(e) });
    } finally {
      finalizeAssistant();
      clearThinking();
      setResponding(false);
    }
  }

  function submit() {
    const text = promptEl.value.trim();
    if (!text || responding) return;
    promptEl.value = "";
    autoResize();
    sendPrompt(text);
  }

  function autoResize() {
    promptEl.style.height = "auto";
    promptEl.style.height = Math.min(promptEl.scrollHeight, 180) + "px";
  }

  sendBtn.addEventListener("click", submit);
  promptEl.addEventListener("input", autoResize);
  promptEl.addEventListener("keydown", function (e) {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      submit();
    }
  });

  stopBtn.addEventListener("click", function () {
    fetch("/api/cancel", { method: "POST", headers: JSON_HEADERS }).catch(
      function () {},
    );
  });

  newChatBtn.addEventListener("click", async function () {
    if (responding) return;
    try {
      await fetch("/api/reset", { method: "POST", headers: JSON_HEADERS });
    } catch (_) {}
    log.innerHTML = "";
    clearThinking();
    promptEl.focus();
  });

  modelSelect.addEventListener("change", async function () {
    const name = modelSelect.value;
    if (!name) return;
    try {
      await fetch("/api/model", {
        method: "POST",
        headers: JSON_HEADERS,
        body: JSON.stringify({ model: name }),
      });
      currentModel = name;
    } catch (_) {}
  });

  async function loadModels() {
    try {
      const r = await fetch("/api/models");
      const data = await r.json();
      currentModel = data.current || "";
      modelSelect.innerHTML = "";
      const models = data.models || [];
      if (!models.length && currentModel) models.push(currentModel);
      models.forEach(function (name) {
        const opt = document.createElement("option");
        opt.value = name;
        opt.textContent = name;
        if (name === currentModel) opt.selected = true;
        modelSelect.appendChild(opt);
      });
    } catch (_) {
      // Leave the selector empty; chatting still works with the configured model.
    }
  }

  autoResize();
  loadModels();
  promptEl.focus();
})();
