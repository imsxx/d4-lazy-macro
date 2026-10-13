// ================= Diablo Lazy Macro 前端交互与 API 驱动 =================

let state = null;
let currentTab = 0;
let capturingTarget = null; // null | { type: 'slot', index: 0 } | { type: 'toggle' } | { type: 'profileHk' } | { type: 'forceStand' }

const strategyNames = ['禁用', '按住不放', '连点', '保持Buff'];

// 初始化入口
window.addEventListener('DOMContentLoaded', async () => {
  try {
    await reloadState();
  } catch (err) {
    console.error('初始化失败:', err);
  }

  // 全局键盘监听，用于按键录入
  window.addEventListener('keydown', handleKeyDown);
});

// 从 Go 后端加载最新状态并渲染
async function reloadState() {
  if (typeof window.api_getState !== 'function') {
    console.warn('Go API 尚未注入，等待中...');
    return;
  }
  state = await window.api_getState();
  currentTab = state.currentProfileIdx || 0;
  renderAll();
}

// 供 Go 后端主动调用的状态更新通知
window.onStateUpdate = function(newState) {
  state = newState;
  renderStatusAndHeader();
};

function renderAll() {
  if (!state) return;
  renderHeader();
  renderTabs();
  renderSlots();
  renderExtraSettings();
  renderHelperSection();
  renderPreferences();
  renderStatusAndHeader();
}

// 渲染顶栏游戏按钮与快捷键
function renderHeader() {
  document.querySelectorAll('.game-btn').forEach(btn => {
    btn.classList.toggle('active', btn.dataset.game === state.currentGame);
  });
  const toggleBtn = document.getElementById('toggleKeyBtn');
  if (toggleBtn) {
    toggleBtn.textContent = state.toggleKey || 'X1';
  }
}

// 渲染方案 Tabs
function renderTabs() {
  const tabs = document.querySelectorAll('.tab-btn');
  tabs.forEach((btn, idx) => {
    btn.classList.toggle('active', idx === currentTab);
    const profName = state.profileNames[idx] || `配置 ${idx + 1}`;
    btn.textContent = profName;
  });
}

// 渲染当前方案的 6 个槽位
function renderSlots() {
  const container = document.getElementById('slotList');
  if (!container) return;
  container.innerHTML = '';

  const slots = state.profiles[currentTab].slots;
  const slotLabels = ['技能一:', '技能二:', '技能三:', '技能四:', '左键技能:', '右键技能:'];

  slots.forEach((s, idx) => {
    const row = document.createElement('div');
    row.className = 'slot-row';

    const spam = s.strategy === 2;
    const buff = s.strategy === 3;
    const enabledInterval = spam || buff;
    const enabledTuning = spam;

    row.innerHTML = `
      <span class="col-slot">${slotLabels[idx]}</span>
      <span class="col-key">
        <button class="keycap-btn sm" onclick="captureSlotKey(${idx})">${s.key || '—'}</button>
      </span>
      <span class="col-strategy">
        <select class="select-dropdown md" onchange="onStrategyChange(${idx}, this.value)">
          ${strategyNames.map((name, sIdx) => `<option value="${sIdx}" ${s.strategy === sIdx ? 'selected' : ''}>${name}</option>`).join('')}
        </select>
      </span>
      <span class="col-interval">
        <input type="number" class="input-num sm" value="${s.intervalMS}" min="20" max="600000"
          ${enabledInterval ? '' : 'disabled'} onchange="onIntervalChange(${idx}, this.value)">
      </span>
      <span class="col-delay">
        <input type="number" class="input-num xs" value="${s.delayMS || ''}" placeholder="0" min="-30000" max="30000"
          ${enabledTuning ? '' : 'disabled'} onchange="onDelayChange(${idx}, this.value)">
      </span>
      <span class="col-random">
        <input type="checkbox" ${s.randomDelay ? 'checked' : ''}
          ${enabledTuning ? '' : 'disabled'} onchange="onRandomChange(${idx}, this.checked)">
      </span>
    `;
    container.appendChild(row);
  });
}

// 渲染左栏额外设置卡片
function renderExtraSettings() {
  const prof = state.profiles[currentTab];
  document.getElementById('profileHkBtn').textContent = prof.hk || '无';
  document.getElementById('profileNameInput').value = prof.name || `配置 ${currentTab + 1}`;
  document.getElementById('profileModeSelect').value = prof.mode || 'concurrent';

  document.getElementById('startModeSelect').value = String(state.startMode || 0);

  const qp = state.quickPause;
  document.getElementById('qpEnabled').checked = qp.enabled;
  document.getElementById('qpTrigger').value = String(qp.trigger);
  document.getElementById('qpMouseKey').value = String(qp.mouseKey);
  document.getElementById('qpAction').value = String(qp.action);
  document.getElementById('qpDelay').value = qp.delayMS;
}

// 渲染右栏城镇助手 (暗黑3显示，非暗黑3隐藏)
function renderHelperSection() {
  const isD3 = state.currentGame === 'd3';
  const helperCard = document.getElementById('helperCard');
  if (helperCard) {
    helperCard.style.display = isD3 ? 'flex' : 'none';
  }
  if (!isD3) return;

  const h = state.helpers;
  document.getElementById('helperHotkeyInput').value = h.hotkey || 'F';
  document.getElementById('helperSpeedSelect').value = String(h.delayMS || 150);
  document.getElementById('reforgeTargetSelect').value = String(h.reforgeTarget || 0);
  document.getElementById('helperDevCheck').checked = !!h.devMode;

  const checklist = document.getElementById('helperChecklist');
  if (checklist && state.allHelpers) {
    checklist.innerHTML = state.allHelpers.map(item => `
      <label class="checkbox-label">
        <input type="checkbox" value="${item.id}" ${h.enabled[item.id] ? 'checked' : ''} onchange="saveHelperSettings()">
        <span>${item.name}</span>
      </label>
    `).join('');
  }
}

// 渲染右栏偏好设置
function renderPreferences() {
  document.getElementById('smartPauseCheck').checked = !!state.smartPause;
  document.getElementById('soundCueCheck').checked = !!state.soundCue;
  document.getElementById('forceStandCheck').checked = !!state.forceStandEnabled;
  document.getElementById('forceStandKeyBtn').textContent = state.forceStandKey || 'Shift';
}

// 渲染底部状态与标题
function renderStatusAndHeader() {
  const profName = state.profileNames[currentTab] || `配置 ${currentTab + 1}`;
  document.getElementById('activeProfileText').textContent = profName;
  document.getElementById('targetProcessText').textContent = state.targetProcess || 'Diablo.exe';

  const pill = document.getElementById('macroStatusPill');
  if (pill) {
    if (state.macroRunning) {
      pill.className = 'status-pill running';
      pill.textContent = `● 宏运行中 (按 ${state.toggleKey} 停止)`;
    } else if (state.macroPaused) {
      pill.className = 'status-pill paused';
      pill.textContent = `⏸ 宏已暂停 (按 ${state.toggleKey} 恢复)`;
    } else {
      pill.className = 'status-pill ready';
      pill.textContent = `● 宏已就绪 (前台生效 · 按 ${state.toggleKey} 启动)`;
    }
  }
}

// ================= 用户交互动作 =================

async function switchGame(gameID) {
  if (state.currentGame === gameID) return;
  await window.api_switchGame(gameID);
  await reloadState();
}

function switchTab(idx) {
  currentTab = idx;
  window.api_switchProfile(idx);
  renderTabs();
  renderSlots();
  renderExtraSettings();
  renderStatusAndHeader();
}

// 槽位字段变更即时落盘
async function onStrategyChange(slotIdx, val) {
  const s = state.profiles[currentTab].slots[slotIdx];
  s.strategy = parseInt(val, 10);
  await sendSlotUpdate(slotIdx);
  renderSlots();
}

async function onIntervalChange(slotIdx, val) {
  const s = state.profiles[currentTab].slots[slotIdx];
  s.intervalMS = Math.max(20, parseInt(val, 10) || 300);
  await sendSlotUpdate(slotIdx);
}

async function onDelayChange(slotIdx, val) {
  const s = state.profiles[currentTab].slots[slotIdx];
  s.delayMS = parseInt(val, 10) || 0;
  await sendSlotUpdate(slotIdx);
}

async function onRandomChange(slotIdx, checked) {
  const s = state.profiles[currentTab].slots[slotIdx];
  s.randomDelay = checked;
  await sendSlotUpdate(slotIdx);
}

async function sendSlotUpdate(slotIdx) {
  const s = state.profiles[currentTab].slots[slotIdx];
  await window.api_updateSlot(currentTab, slotIdx, s.key, s.strategy, s.intervalMS, s.delayMS, s.randomDelay);
  flashAutoSave();
}

async function saveProfileName() {
  const name = document.getElementById('profileNameInput').value.trim() || `配置 ${currentTab + 1}`;
  state.profiles[currentTab].name = name;
  state.profileNames[currentTab] = name;
  await window.api_updateProfileMeta(currentTab, name, state.profiles[currentTab].hk, state.profiles[currentTab].mode);
  renderTabs();
  renderStatusAndHeader();
  flashAutoSave();
}

async function saveProfileMode() {
  const mode = document.getElementById('profileModeSelect').value;
  state.profiles[currentTab].mode = mode;
  await window.api_updateProfileMeta(currentTab, state.profiles[currentTab].name, state.profiles[currentTab].hk, mode);
  flashAutoSave();
}

async function saveStartMode() {
  const mode = parseInt(document.getElementById('startModeSelect').value, 10);
  state.startMode = mode;
  await window.api_updateStartMode(mode);
  flashAutoSave();
}

async function saveQuickPause() {
  const enabled = document.getElementById('qpEnabled').checked;
  const trigger = parseInt(document.getElementById('qpTrigger').value, 10);
  const mouseKey = parseInt(document.getElementById('qpMouseKey').value, 10);
  const action = parseInt(document.getElementById('qpAction').value, 10);
  const delay = Math.max(500, Math.min(5000, parseInt(document.getElementById('qpDelay').value, 10) || 1000));
  await window.api_updateQuickPause(enabled, trigger, mouseKey, action, delay);
  flashAutoSave();
}

async function saveHelperSettings() {
  const hotkey = document.getElementById('helperHotkeyInput').value.trim() || 'F';
  const speed = parseInt(document.getElementById('helperSpeedSelect').value, 10);
  const target = parseInt(document.getElementById('reforgeTargetSelect').value, 10);
  const dev = document.getElementById('helperDevCheck').checked;

  const enabled = [];
  document.querySelectorAll('#helperChecklist input[type="checkbox"]:checked').forEach(cb => {
    enabled.push(cb.value);
  });

  await window.api_updateHelpers(hotkey, speed, target, dev, enabled);
  flashAutoSave();
}

async function savePreferences() {
  const smart = document.getElementById('smartPauseCheck').checked;
  const sound = document.getElementById('soundCueCheck').checked;
  const force = document.getElementById('forceStandCheck').checked;
  await window.api_updatePreference(smart, sound, force, state.forceStandKey || 'Shift');
  flashAutoSave();
}

async function saveForceStand() {
  await savePreferences();
}

// ================= 按键捕获模态系统 =================

function openCaptureModal(target) {
  capturingTarget = target;
  document.getElementById('captureOverlay').style.display = 'flex';
}

function closeCaptureModal() {
  capturingTarget = null;
  document.getElementById('captureOverlay').style.display = 'none';
}

function captureSlotKey(slotIdx) {
  openCaptureModal({ type: 'slot', index: slotIdx });
}

function captureToggleKey() {
  openCaptureModal({ type: 'toggle' });
}

function captureProfileHk() {
  openCaptureModal({ type: 'profileHk' });
}

function captureForceStandKey() {
  openCaptureModal({ type: 'forceStand' });
}

async function handleKeyDown(e) {
  if (!capturingTarget) return;

  e.preventDefault();
  e.stopPropagation();

  if (e.key === 'Escape') {
    closeCaptureModal();
    return;
  }

  // 映射按键名
  let token = e.key.toUpperCase();
  if (token === ' ') token = 'SPACE';
  if (token === 'CONTROL') token = 'LCTRL';
  if (token === 'SHIFT') token = 'LSHIFT';
  if (token === 'ALT') token = 'LALT';

  const t = capturingTarget;
  closeCaptureModal();

  if (t.type === 'slot') {
    state.profiles[currentTab].slots[t.index].key = token;
    await sendSlotUpdate(t.index);
    renderSlots();
  } else if (t.type === 'toggle') {
    state.toggleKey = token;
    await window.api_updateToggle(token);
    renderHeader();
  } else if (t.type === 'profileHk') {
    state.profiles[currentTab].hk = token;
    await window.api_updateProfileMeta(currentTab, state.profiles[currentTab].name, token, state.profiles[currentTab].mode);
    renderExtraSettings();
  } else if (t.type === 'forceStand') {
    state.forceStandKey = token;
    await window.api_updatePreference(state.smartPause, state.soundCue, state.forceStandEnabled, token);
    renderPreferences();
  }
}

// 快捷操作
async function importCode() {
  const ok = await window.api_importClipboard();
  if (ok) await reloadState();
}

async function exportCode() {
  await window.api_exportClipboard();
}

function openLog() {
  window.api_openLogWindow();
}

function minimizeWindow() {
  window.api_minimize();
}

function closeWindow() {
  window.api_close();
}

function flashAutoSave() {
  const el = document.getElementById('autoSaveIndicator');
  if (el) {
    el.style.opacity = '1';
    el.textContent = '✓ 实时生效';
  }
}
