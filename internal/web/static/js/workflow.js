import { escapeHTML } from './utils.js';

export function createWorkflowController({
  state, el, api, showToast, openModal, closeModal,
  loadNovels, loadChapters, beginJob,
}) {
  let translationSlug = null;
  function triggerQuickTranslate(slug, start, end) {
    if (!slug) return;
    translationSlug = slug;
    const novel = state.currentNovel?.slug === slug ? state.currentNovel
      : (state.novels || []).find(item => item.slug === slug);
    el.transGenre.value = novel?.genre || '';
    el.transStart.value = start;
    el.transEnd.value = end;
    el.transProgressBox.classList.add('hidden');
    el.transErrorMsg.classList.add('hidden');
    const providerID = el.transProviderSelect?.value || state.translationProvider || state.activeProvider;
    const savedModel = localStorage.getItem(`nc_model_${providerID}`) || '';
    if (savedModel && [...el.transModelSelect.options].some(option => option.value === savedModel)) {
      el.transModelSelect.value = savedModel;
    }
    openModal(el.modalTranslate);
  }

  function openImportModal() {
    openModal(el.modalImport);
    populatePasteNovelSelect();
  }

  function populatePasteNovelSelect() {
    if (!el.pasteNovelSelect) return;
    const options = ['<option value="__new__">+ สร้างนิยายเรื่องใหม่...</option>'];
    for (const novel of state.novels || []) {
      const selected = state.currentSlug === novel.slug ? 'selected' : '';
      const nextChapter = (novel.totalChapters || 0) + 1;
      options.push(`<option value="${escapeHTML(novel.slug)}" ${selected}>${escapeHTML(novel.translatedTitle || novel.title)} (ตอนต่อไป: ${nextChapter})</option>`);
    }
    el.pasteNovelSelect.innerHTML = options.join('');
    updatePasteFormFields();
  }

  function updatePasteFormFields() {
    const selected = el.pasteNovelSelect?.value || '__new__';
    const isNew = selected === '__new__';
    if (el.pasteNewNovelFields) el.pasteNewNovelFields.style.display = isNew ? 'block' : 'none';
    el.pasteSlug.required = isNew;
    if (isNew) {
      el.pasteChNum.value = 1;
      return;
    }
    const novel = (state.novels || []).find(item => item.slug === selected);
    if (novel) el.pasteChNum.value = (novel.totalChapters || 0) + 1;
  }

  function switchImportTab(mode) {
    const paste = mode === 'paste';
    el.tabImportPaste.className = paste ? 'btn btn-primary btn-sm' : 'btn btn-outline btn-sm';
    el.tabImportUrl.className = paste ? 'btn btn-outline btn-sm' : 'btn btn-primary btn-sm';
    el.formImportPaste.classList.toggle('hidden', !paste);
    el.formImportUrl.classList.toggle('hidden', paste);
    if (paste) populatePasteNovelSelect();
  }
  async function submitURLImport(event) {
    event.preventDefault();
    const url = el.importUrl?.value.trim() || '';
    const genre = el.importGenre.value;
    const startChapter = Number.parseInt(el.importStart?.value, 10) || 1;
    const endChapter = Number.parseInt(el.importEnd?.value, 10) || 0;
    if (!url) {
      showToast('กรุณาระบุ URL ต้นฉบับ', 'warning');
      return;
    }
    closeModal(el.modalImport);
    showToast('เริ่มดาวน์โหลดนิยายจาก URL แล้ว...', 'info');
    try {
      const res = await api('/api/import', {
        method: 'POST',
        body: JSON.stringify({ url, genre, startChapter, endChapter }),
      });
      if (res.jobId) {
        beginJob(res.jobId, 'import');
        el.topProgressBar?.classList.remove('hidden');
        el.floatingJobBar?.classList.remove('hidden');
        if (el.floatJobTitle) el.floatJobTitle.textContent = 'กำลังเตรียมนำเข้านิยาย...';
        if (el.floatJobPct) el.floatJobPct.textContent = '0%';
      }
    } catch (err) {
      console.error('URL import failed', err);
    }
  }

  function fallbackSlug(title) {
    const ascii = title.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '');
    return ascii || `novel-${Date.now()}`;
  }
  async function submitPasteImport(event) {
    event.preventDefault();
    const selected = el.pasteNovelSelect?.value || '__new__';
    let slug = selected;
    let novelTitle = '';
    let genre = 'apocalypse';
    if (selected === '__new__') {
      novelTitle = el.pasteNovelTitle.value.trim();
      slug = el.pasteSlug.value.trim() || fallbackSlug(novelTitle);
      if (!novelTitle && !el.pasteSlug.value.trim()) {
        showToast('กรุณาระบุชื่อเรื่องหรือ Slug', 'error');
        return;
      }
      genre = el.pasteGenre.value;
    } else {
      const novel = (state.novels || []).find(item => item.slug === slug);
      if (novel) genre = novel.genre || genre;
    }

    const chapterNo = Number.parseInt(el.pasteChNum.value, 10) || 1;
    const title = el.pasteTitle.value.trim() || `ตอนที่ ${chapterNo}`;
    const rawContent = el.pasteContent.value.trim();
    if (!rawContent) {
      showToast('กรุณาวางเนื้อหาบทความ', 'error');
      return;
    }
    try {
      await api('/api/import', {
        method: 'POST',
        body: JSON.stringify({ novelSlug: slug, novelTitle, title, genre, startChapter: chapterNo, rawContent }),
      });
      closeModal(el.modalImport);
      el.pasteContent.value = '';
      el.pasteTitle.value = '';
      showToast(`บันทึกตอนที่ ${chapterNo} เรียบร้อยแล้ว`, 'success');
      await loadNovels();
      if (state.currentSlug === slug) await loadChapters(slug);
    } catch (err) {
      console.error('paste import failed', err);
    }
  }

  function showTranslationStarting(start, end) {
    el.topProgressBar.classList.remove('hidden');
    el.topProgressBar.style.width = '5%';
    el.floatingJobBar.classList.remove('hidden');
    el.floatJobTitle.textContent = `กำลังแปลตอนที่ ${start}... (0/${end - start + 1})`;
    el.floatJobPct.textContent = '0%';
    el.floatJobBar.style.width = '5%';
  }

  function hideTranslationStarting() {
    el.topProgressBar.classList.add('hidden');
    el.floatingJobBar.classList.add('hidden');
  }
  async function submitTranslation(event) {
    event.preventDefault();
    if (!state.currentSlug) return;
    const startChapter = Number.parseInt(el.transStart.value, 10) || 1;
    const endChapter = Number.parseInt(el.transEnd.value, 10) || startChapter;
    const provider = el.transProviderSelect?.value || state.translationProvider || state.activeProvider;
    const model = el.transModelSelect.value.trim();
    const genre = el.transGenre.value;
    const force = Boolean(el.transForce.checked);
    const fallbackModels = (state.availableModels || [])
      .filter(candidate => candidate && candidate !== model)
      .slice(0, 3);

    if (!model) {
      showToast('กรุณาเลือกโมเดลก่อนเริ่มแปล', 'warning');
      return;
    }
    state.translationProvider = provider;
    localStorage.setItem('nc_translate_provider', provider);
    localStorage.setItem(`nc_model_${provider}`, model);
    closeModal(el.modalTranslate);
    showToast(`เริ่มคิวแปลตอนที่ ${startChapter} - ${endChapter} ในพื้นหลังแล้ว`, 'info');
    showTranslationStarting(startChapter, endChapter);
    try {
      const res = await api('/api/translate', {
        method: 'POST',
        body: JSON.stringify({
          novelSlug: translationSlug || state.currentSlug,
          provider,
          startChapter,
          endChapter,
          model,
          fallbackModels,
          genre,
          force,
        }),
      });
      if (res.jobId) beginJob(res.jobId, 'translation');
    } catch (err) {
      hideTranslationStarting();
      showToast(`เริ่มงานแปลไม่สำเร็จ: ${err.message}`, 'error');
    }
  }

  function bindWorkflowEvents() {
    el.btnOpenImport?.addEventListener('click', openImportModal);
    el.btnCloseImport?.addEventListener('click', () => closeModal(el.modalImport));
    el.tabImportUrl?.addEventListener('click', () => switchImportTab('url'));
    el.tabImportPaste?.addEventListener('click', () => switchImportTab('paste'));
    el.pasteNovelSelect?.addEventListener('change', updatePasteFormFields);
    el.formImportUrl?.addEventListener('submit', submitURLImport);
    el.formImportPaste?.addEventListener('submit', submitPasteImport);
    el.btnCloseTranslate?.addEventListener('click', () => closeModal(el.modalTranslate));
    el.formTranslate?.addEventListener('submit', submitTranslation);
    el.btnQuickTranslate?.addEventListener('click', () => {
      const last = state.chapters?.length
        ? state.chapters[state.chapters.length - 1].chapterNo
        : 10;
      triggerQuickTranslate(state.currentSlug, 1, Math.min(10, last));
    });
    el.btnReaderTranslate?.addEventListener('click', () => {
      triggerQuickTranslate(state.currentSlug, state.currentChapterNo, state.currentChapterNo);
    });
  }

  return {
    triggerQuickTranslate,
    openImportModal,
    populatePasteNovelSelect,
    bindWorkflowEvents,
  };
}
