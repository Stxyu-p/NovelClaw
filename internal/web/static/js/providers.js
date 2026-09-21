import { clampInt, escapeHTML, uniqueNonEmpty } from './utils.js';

export function createProviderController({ state, el, api }) {
  const discoveryVersions = new Map();
  let testVersion = 0;
  function getProvider(id) {
    return state.providers.find(provider => provider.id === id) || null;
  }

  function renderProviderStatus() {
    const provider = getProvider(state.activeProvider);
    if (!el.activeProviderLabel || !el.activeModelLabel) return;
    const ready = Boolean(provider?.configured && state.defaultModel);
    el.activeProviderLabel.textContent = provider?.name || state.activeProvider || 'ยังไม่ได้ตั้ง Provider';
    el.activeModelLabel.textContent = provider?.configured
      ? (state.defaultModel || 'ยังไม่ได้เลือกโมเดล')
      : 'ต้องตั้งค่า Provider / API Key';
    if (!el.btnProviderStatus) return;
    el.btnProviderStatus.classList.toggle('is-ready', ready);
    el.btnProviderStatus.classList.toggle('needs-setup', Boolean(provider && !ready));
    el.btnProviderStatus.title = ready
      ? `${provider.name} • ${state.defaultModel}`
      : 'AI Provider ยังตั้งค่าไม่ครบ — คลิกเพื่อแก้ไข';
  }

  function providerFreeModels(provider) {
    return uniqueNonEmpty([
      ...(provider?.liveFreeModels || []),
      ...(provider?.freeModels || []),
    ]);
  }

  function isFreeModel(provider, model) {
    const id = String(model || '').trim().toLowerCase();
    if (!id) return false;
    return providerFreeModels(provider).includes(model)
      || id === 'openrouter/free'
      || id.endsWith(':free')
      || id.endsWith('-free')
      || (provider?.id === 'opencode-zen' && id === 'big-pickle');
  }

  function providerCandidateModels(provider, remoteModels = []) {
    if (!provider) return remoteModels;
    return uniqueNonEmpty([
      provider.model,
      ...providerFreeModels(provider),
      ...(remoteModels || []),
      ...(provider.modelHints || []),
    ]);
  }

  function renderModelOptions(models, selected, provider) {
    const option = model => `<option value="${escapeHTML(model)}" ${model === selected ? 'selected' : ''}>${isFreeModel(provider, model) ? '🆓 ' : ''}${escapeHTML(model)}</option>`;
    const free = models.filter(model => isFreeModel(provider, model));
    const regular = models.filter(model => !isFreeModel(provider, model));
    if (!free.length) return regular.map(option).join('');
    const freeGroup = `<optgroup label="🆓 ฟรี">${free.map(option).join('')}</optgroup>`;
    const regularGroup = regular.length ? `<optgroup label="โมเดลอื่น">${regular.map(option).join('')}</optgroup>` : '';
    return freeGroup + regularGroup;
  }

  function renderFreeModelHints(provider) {
    const free = providerFreeModels(provider);
    if (!free.length) {
      el.providerFreeModels.innerHTML = '';
      return;
    }
    const keyNote = provider?.keyRequired ? ' \u00b7 \u0e15\u0e49\u0e2d\u0e07\u0e43\u0e0a\u0e49 API Key' : '';
    const currentModel = (el.cfgModelCustom.value || el.cfgModelSelect.value || provider?.model || '').trim();
    const button = model => {
      const active = model === currentModel;
      return `<button type="button" class="model-hint${active ? ' is-active' : ''}" data-model="${escapeHTML(model)}" aria-pressed="${active ? 'true' : 'false'}">${escapeHTML(model)}</button>`;
    };
    el.providerFreeModels.innerHTML = `<div class="free-model-head"><strong>\u{1F193} \u0e42\u0e21\u0e40\u0e14\u0e25\u0e1f\u0e23\u0e35\u0e17\u0e31\u0e49\u0e07\u0e2b\u0e21\u0e14 ${free.length} \u0e23\u0e38\u0e48\u0e19</strong><span>${keyNote.replace(' \u00b7 ', '')}</span></div><div class="free-model-grid">${free.map(button).join('')}</div>`;
  }

  function renderProviderOptions() {
    el.cfgProvider.innerHTML = state.providers.map(provider => {
      const scope = provider.local ? 'Local' : 'Cloud';
      const ready = provider.configured ? ' • ตั้งค่าแล้ว' : '';
      return `<option value="${escapeHTML(provider.id)}">${escapeHTML(provider.name)} — ${scope}${ready}</option>`;
    }).join('');
  }

  function renderTranslationProviderOptions(preferred = '') {
    if (!el.transProviderSelect) return;
    const selected = preferred || state.translationProvider || state.activeProvider;
    el.transProviderSelect.innerHTML = state.providers.map(provider => {
      const scope = provider.local ? 'Local' : 'Cloud';
      const ready = provider.configured ? 'พร้อมใช้' : 'ยังไม่ตั้งค่า';
      return `<option value="${escapeHTML(provider.id)}">${escapeHTML(provider.name)} — ${scope} • ${ready}</option>`;
    }).join('');
    if (getProvider(selected)) el.transProviderSelect.value = selected;
    state.translationProvider = el.transProviderSelect.value || state.activeProvider;
    updateTranslationProviderHint();
  }

  function updateTranslationProviderHint() {
    if (!el.transProviderHint) return;
    const provider = getProvider(state.translationProvider || el.transProviderSelect?.value);
    if (!provider) {
      el.transProviderHint.textContent = 'ยังไม่ได้เลือก Provider';
      return;
    }
    const scope = provider.local ? 'Local' : 'Cloud';
    const status = provider.configured ? 'พร้อมใช้งาน' : 'ยังตั้งค่าไม่ครบ — เปิด Settings เพื่อเพิ่ม URL / API Key';
    el.transProviderHint.textContent = `${provider.name} • ${scope} • ${status}`;
    el.transProviderHint.classList.toggle('is-warning', !provider.configured);
  }

  function renderSettingsModelOptions(provider, remoteModels = []) {
    const selected = (el.cfgModelCustom.value || provider?.model || '').trim();
    const models = providerCandidateModels(provider, remoteModels);
    el.cfgModelSelect.innerHTML = models.length
      ? renderModelOptions(models, selected, provider)
      : '<option value="">ยังไม่มีรายชื่อโมเดล — ทดสอบการเชื่อมต่อหรือพิมพ์ Model ID</option>';
  }

  function populateTranslationModels(providerID = state.translationProvider || state.activeProvider, remoteModels = state.availableModels) {
    const provider = getProvider(providerID);
    const savedModel = localStorage.getItem(`nc_model_${providerID}`) || '';
    const providerDefault = providerID === state.activeProvider ? state.defaultModel : (provider?.model || '');
    const selected = savedModel || providerDefault;
    const models = providerCandidateModels(provider, remoteModels);
    if (selected && !models.includes(selected)) models.unshift(selected);
    el.transModelSelect.innerHTML = models.length
      ? renderModelOptions(models, selected, provider)
      : '<option value="">ยังไม่มีโมเดลที่พร้อมใช้งาน</option>';
    if (selected && models.includes(selected)) el.transModelSelect.value = selected;
  }

  function applyProviderToSettings(providerID) {
    const provider = getProvider(providerID);
    if (!provider) return;
    state.settingsProvider = provider.id;
    state.clearProviderKey = false;
    el.cfgProvider.value = provider.id;
    el.cfgRouterUrl.value = provider.profileUrl || provider.baseUrl || '';
    el.cfgApiKey.value = '';
    el.cfgApiKey.placeholder = provider.keyMasked
      ? `ใช้ Key ที่จำไว้ (${provider.keyMasked})`
      : (provider.keyRequired ? 'ใส่ API Key' : 'ไม่จำเป็นต้องใช้ Key');
    el.cfgKeyStatus.textContent = provider.keyMasked
      ? `บันทึกแล้ว ${provider.keyMasked}`
      : (provider.keyRequired ? 'ยังไม่ได้ตั้ง Key' : 'ไม่บังคับใช้ Key');
    el.cfgKeyStatus.className = `key-status ${provider.keyMasked ? 'is-set' : ''}`;
    el.cfgModelCustom.value = '';
    el.cfgProtocol.value = provider.profileProtocol || provider.protocol || 'openai-chat';
    el.cfgProtocolWrap.hidden = provider.id !== 'custom';
    renderSettingsModelOptions(provider);

    const badges = [
      provider.local ? 'Local' : 'Cloud',
      provider.keyRequired ? 'ต้องใช้ API Key' : 'Key ไม่บังคับ',
    ];
    el.providerSummary.innerHTML = `<div><strong>${escapeHTML(provider.name)}</strong><span>${escapeHTML(provider.description || '')}</span></div><div>${badges.map(value => `<span class="provider-pill">${escapeHTML(value)}</span>`).join('')}</div>`;
    renderFreeModelHints(provider);
  }

  async function discoverModels(providerID = state.activeProvider, options = {}) {
    const version = (discoveryVersions.get(providerID) || 0) + 1;
    discoveryVersions.set(providerID, version);
    const updateTranslation = options.updateTranslation ?? providerID === (state.translationProvider || state.activeProvider);
    const quiet = options.quiet ?? false;
    const provider = getProvider(providerID);
    try {
      const res = await api(`/api/models?provider=${encodeURIComponent(providerID)}`, { silent: quiet });
      const models = res.models || [];
      if (discoveryVersions.get(providerID) !== version) return models;
      if (provider) provider.liveFreeModels = res.freeModels || [];
      if (updateTranslation && state.translationProvider === providerID) {
        state.availableModels = models;
        state.translationProvider = providerID;
        renderTranslationProviderOptions(providerID);
        populateTranslationModels(providerID, models);
      }
      if (providerID === state.settingsProvider) {
        renderSettingsModelOptions(provider, models);
        renderFreeModelHints(provider);
      }
      return models;
    } catch (err) {
      if (discoveryVersions.get(providerID) !== version) return [];
      if (!quiet) console.warn('Model discovery warning:', err);
      if (provider) provider.liveFreeModels = [];
      if (updateTranslation && state.translationProvider === providerID) {
        state.availableModels = [];
        state.translationProvider = providerID;
        renderTranslationProviderOptions(providerID);
        populateTranslationModels(providerID, []);
      }
      if (providerID === state.settingsProvider) {
        renderSettingsModelOptions(provider);
        renderFreeModelHints(provider);
      }
      return [];
    }
  }

  async function loadConfigAndModels() {
    try {
      const [cfg, providerRes] = await Promise.all([
        api('/api/config'),
        api('/api/providers'),
      ]);
      state.providers = providerRes.providers || [];
      state.activeProvider = cfg.provider || providerRes.active || 'custom';
      state.defaultModel = cfg.defaultModel || '';
      if (!getProvider(state.translationProvider)) state.translationProvider = state.activeProvider;
      localStorage.setItem('nc_model', state.defaultModel);
      localStorage.setItem('nc_translate_provider', state.translationProvider);
      renderProviderOptions();
      renderTranslationProviderOptions(state.translationProvider);
      renderProviderStatus();
      applyProviderToSettings(state.activeProvider);
      el.cfgTemp.value = cfg.temperature ?? 0.3;
      el.cfgParallel.value = cfg.parallel ?? 2;
      el.cfgMaxTokens.value = cfg.maxTokens ?? 8192;
      // Reading a local book should not make an unsolicited cloud request.
      // Saved models and provider hints work offline; discovery stays explicit.
      populateTranslationModels(state.translationProvider, []);
    } catch (err) {
      console.warn('Config load warning:', err);
      populateTranslationModels();
    }
  }

  function currentSettingsPayload({ includeTuning = true } = {}) {
    const provider = getProvider(state.settingsProvider);
    const payload = {
      provider: state.settingsProvider,
      routerUrl: el.cfgRouterUrl.value.trim(),
      defaultModel: el.cfgModelCustom.value.trim() || el.cfgModelSelect.value || provider?.model || '',
      protocol: provider?.id === 'custom'
        ? el.cfgProtocol.value
        : (provider?.profileProtocol || provider?.protocol || ''),
    };
    const typedKey = el.cfgApiKey.value.trim();
    if (state.clearProviderKey) payload.apiKey = '';
    else if (typedKey) payload.apiKey = typedKey;
    if (includeTuning) {
      const temperature = Number.parseFloat(el.cfgTemp.value);
      payload.temperature = Number.isFinite(temperature) ? Math.max(0, Math.min(2, temperature)) : 0.3;
      payload.parallel = clampInt(el.cfgParallel.value, 1, 8, 2);
      payload.maxTokens = clampInt(el.cfgMaxTokens.value, 512, 131072, 8192);
    }
    return payload;
  }

  async function refreshProviderControlPlane(preferredProvider = '') {
    const [cfg, providerRes] = await Promise.all([
      api('/api/config', { silent: true }),
      api('/api/providers', { silent: true }),
    ]);
    state.providers = providerRes.providers || [];
    state.activeProvider = cfg.provider || providerRes.active || 'custom';
    state.defaultModel = cfg.defaultModel || '';
    renderProviderOptions();
    renderProviderStatus();
    const selected = preferredProvider && getProvider(preferredProvider)
      ? preferredProvider
      : state.activeProvider;
    applyProviderToSettings(selected);
    el.cfgTemp.value = cfg.temperature ?? 0.3;
    el.cfgParallel.value = cfg.parallel ?? 2;
    el.cfgMaxTokens.value = cfg.maxTokens ?? 8192;
    return cfg;
  }

  async function testCurrentProvider() {
    const version = ++testVersion;
    const provider = getProvider(state.settingsProvider);
    if (!provider) return [];
    el.providerTestResult.className = 'provider-test-result is-loading';
    el.providerTestResult.textContent = `กำลังทดสอบ ${provider.name}...`;
    el.btnTestProvider.disabled = true;
    try {
      const res = await api('/api/providers/test', {
        method: 'POST',
        body: JSON.stringify(currentSettingsPayload({ includeTuning: false })),
      });
      const models = res.models || [];
      if (version !== testVersion || state.settingsProvider !== provider.id) return models;
      provider.liveFreeModels = res.freeModels || [];
      renderSettingsModelOptions(provider, models);
      renderFreeModelHints(provider);
      el.providerTestResult.className = 'provider-test-result is-success';
      el.providerTestResult.textContent = `อ่านรายชื่อสำเร็จ • ${models.length} โมเดล • ยังไม่ได้ทดสอบสร้างคำแปล`;
      return models;
    } catch (err) {
      if (version !== testVersion || state.settingsProvider !== provider.id) return [];
      el.providerTestResult.className = 'provider-test-result is-error';
      el.providerTestResult.textContent = `เชื่อมต่อไม่สำเร็จ: ${err.message}`;
      return [];
    } finally {
      if (version === testVersion) el.btnTestProvider.disabled = false;
    }
  }

  return {
    getProvider,
    renderProviderStatus,
    renderTranslationProviderOptions,
    updateTranslationProviderHint,
    populateTranslationModels,
    applyProviderToSettings,
    loadConfigAndModels,
    discoverModels,
    currentSettingsPayload,
    refreshProviderControlPlane,
    testCurrentProvider,
  };
}
