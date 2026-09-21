import { escapeHTML } from './utils.js';

export function createProviderEvents({
  state, el, api, showToast, openModal, closeModal,
  getProvider, renderProviderStatus, applyProviderToSettings,
  renderTranslationProviderOptions, updateTranslationProviderHint,
  discoverModels, testCurrentProvider, refreshProviderControlPlane,
  currentSettingsPayload,
}) {
  async function refreshTranslationModels() {
    const providerID = el.transProviderSelect?.value || state.translationProvider || state.activeProvider;
    const provider = getProvider(providerID);
    showToast(`กำลังดึงโมเดลจาก ${provider?.name || 'Provider'}...`, 'info');
    const models = await discoverModels(providerID, { updateTranslation: true });
    showToast(`พบโมเดล ${models.length} รายการ`, models.length ? 'success' : 'warning');
  }

  function rememberTranslationModel() {
    const providerID = el.transProviderSelect?.value || state.translationProvider || state.activeProvider;
    const chosen = el.transModelSelect.value;
    if (!providerID || !chosen) return;
    localStorage.setItem(`nc_model_${providerID}`, chosen);
  }

  async function changeTranslationProvider() {
    const providerID = el.transProviderSelect?.value || state.activeProvider;
    state.translationProvider = providerID;
    localStorage.setItem('nc_translate_provider', providerID);
    renderTranslationProviderOptions(providerID);
    updateTranslationProviderHint();
    el.transModelSelect.innerHTML = '<option value="">กำลังโหลดรายชื่อโมเดล...</option>';
    await discoverModels(providerID, { updateTranslation: true, quiet: true });
  }

  async function openSettings() {
    openModal(el.modalSettings);
    el.providerTestResult.textContent = '';
    try {
      await refreshProviderControlPlane();
      await discoverModels(state.settingsProvider, { updateTranslation: false, quiet: true });
    } catch (err) {
      el.providerTestResult.className = 'provider-test-result is-error';
      el.providerTestResult.textContent = `โหลดการตั้งค่าไม่สำเร็จ: ${err.message}`;
    }
  }
  async function detectLocalProviders() {
    el.detectProviders.innerHTML = '<span class="provider-pill">กำลังตรวจ Local gateway...</span>';
    try {
      const res = await api('/api/detect-providers');
      const found = res.providers || [];
      if (!found.length) {
        el.detectProviders.innerHTML = '<span class="provider-muted">ไม่พบ Local gateway ที่กำลังทำงาน</span>';
        return;
      }
      el.detectProviders.innerHTML = found.map(provider => `
        <button type="button" class="provider-detected-item" data-provider="${escapeHTML(provider.provider)}" data-url="${escapeHTML(provider.url)}">
          <span>● ${escapeHTML(provider.provider)}</span><small>${provider.modelCount || 0} models</small>
        </button>`).join('');
    } catch (err) {
      el.detectProviders.innerHTML = '<span class="provider-error">ตรวจ Local gateway ไม่สำเร็จ</span>';
    }
  }

  async function activateDetectedProvider(button) {
    applyProviderToSettings(button.dataset.provider);
    el.cfgRouterUrl.value = button.dataset.url || el.cfgRouterUrl.value;
    await discoverModels(state.settingsProvider, { updateTranslation: false, quiet: true });
  }
  async function saveSettings(event) {
    event.preventDefault();
    const payload = currentSettingsPayload();
    if (!payload.routerUrl) {
      showToast('กรุณาระบุ Base URL', 'warning');
      return;
    }
    if (!payload.defaultModel) {
      showToast('กรุณาเลือกหรือระบุ Model ID', 'warning');
      return;
    }
    el.btnSaveSettings.disabled = true;
    el.btnSaveSettings.textContent = 'กำลังบันทึก...';
    try {
      const cfg = await api('/api/config', { method: 'POST', body: JSON.stringify(payload) });
      state.activeProvider = cfg.provider;
      state.defaultModel = cfg.defaultModel || payload.defaultModel;
      localStorage.setItem('nc_model', state.defaultModel);
      await refreshProviderControlPlane(cfg.provider);
      await discoverModels(cfg.provider, { updateTranslation: state.translationProvider === cfg.provider, quiet: true });
      renderTranslationProviderOptions(state.translationProvider || cfg.provider);
      updateTranslationProviderHint();
      showToast(`ใช้งาน ${getProvider(cfg.provider)?.name || cfg.provider} แล้ว`, 'success');
      closeModal(el.modalSettings);
    } catch (err) {
      el.providerTestResult.className = 'provider-test-result is-error';
      el.providerTestResult.textContent = `บันทึกการตั้งค่าไม่สำเร็จ: ${err.message}`;
    } finally {
      el.btnSaveSettings.disabled = false;
      el.btnSaveSettings.textContent = '✓ บันทึกและใช้งาน';
    }
  }
  function bindProviderEvents() {
    el.btnRefreshModels?.addEventListener('click', refreshTranslationModels);
    el.btnCfgRefreshModels?.addEventListener('click', testCurrentProvider);
    el.transProviderSelect?.addEventListener('change', changeTranslationProvider);
    el.transModelSelect?.addEventListener('change', rememberTranslationModel);
    el.btnProviderStatus?.addEventListener('click', () => el.btnOpenSettings.click());
    el.btnOpenSettings?.addEventListener('click', openSettings);
    el.btnCloseSettings?.addEventListener('click', () => closeModal(el.modalSettings));
    el.cfgProvider?.addEventListener('change', async () => {
      applyProviderToSettings(el.cfgProvider.value);
      el.providerTestResult.textContent = '';
      await discoverModels(state.settingsProvider, { updateTranslation: false, quiet: true });
    });
    el.cfgModelSelect?.addEventListener('change', () => {
      if (el.cfgModelSelect.value) el.cfgModelCustom.value = '';
      el.providerFreeModels?.querySelectorAll('[data-model]').forEach(button => {
        const active = button.dataset.model === el.cfgModelSelect.value;
        button.classList.toggle('is-active', active);
        button.setAttribute('aria-pressed', active ? 'true' : 'false');
      });
    });
    el.providerFreeModels?.addEventListener('click', event => {
      const button = event.target.closest('[data-model]');
      if (!button) return;
      const model = button.dataset.model || '';
      const inSelect = [...el.cfgModelSelect.options].some(option => option.value === model);
      if (inSelect) {
        el.cfgModelSelect.value = model;
        el.cfgModelCustom.value = '';
      } else {
        el.cfgModelCustom.value = model;
      }
      el.providerFreeModels.querySelectorAll('[data-model]').forEach(item => {
        const active = item.dataset.model === model;
        item.classList.toggle('is-active', active);
        item.setAttribute('aria-pressed', active ? 'true' : 'false');
      });
      showToast(`เลือก ${model} แล้ว`, 'info');
    });
    el.btnClearApiKey?.addEventListener('click', () => {
      state.clearProviderKey = true;
      el.cfgApiKey.value = '';
      el.cfgKeyStatus.textContent = 'จะล้าง Key เมื่อกดบันทึก';
      el.cfgKeyStatus.className = 'key-status is-warning';
    });
    el.cfgApiKey?.addEventListener('input', () => {
      if (!el.cfgApiKey.value.trim()) return;
      state.clearProviderKey = false;
      el.cfgKeyStatus.textContent = 'มี Key ใหม่รอบันทึก';
      el.cfgKeyStatus.className = 'key-status is-pending';
    });
    el.btnTestProvider?.addEventListener('click', testCurrentProvider);
    el.btnDetectProviders?.addEventListener('click', detectLocalProviders);
    el.detectProviders?.addEventListener('click', event => {
      const button = event.target.closest('[data-provider]');
      if (button) activateDetectedProvider(button);
    });
    el.formSettings?.addEventListener('submit', saveSettings);
  }

  return { bindProviderEvents };
}
