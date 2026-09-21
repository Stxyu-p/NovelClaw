import { escapeHTML } from './utils.js';

export function createReaderController({
  state, el, api, showView, showToast,
  maxChapterNo, adjacentChapterNo, openNovelDetail, triggerQuickTranslate, stopTTS,
  saveReadingPosition = () => {},
  saveBookmark = (slug, chapterNo) => api(`/api/novels/${encodeURIComponent(slug)}/bookmark`, {
    method: 'POST', body: JSON.stringify({ chapterNo, scrollPercentage: 0 }), silent: true,
  }),
}) {
  let requestID = 0;
  let chapterRequest = null;
  let restoreTimer = null;

  function cancelPendingLoad() {
    requestID += 1;
    chapterRequest?.abort();
    clearTimeout(restoreTimer);
  }

  async function openChapter(slug, chapterNo) {
    saveReadingPosition();
    if (state.tts.speaking) stopTTS();
    state.currentSlug = slug;
    state.currentChapterNo = chapterNo;
    state.currentChapterData = null;
    showView('reader');
    const loaded = await loadChapterContent(slug, chapterNo);
    if (!loaded) return false;
    saveBookmark(slug, chapterNo).catch(err => console.warn('bookmark save failed', err));
    return state.currentView === 'reader' && state.currentSlug === slug && state.currentChapterNo === chapterNo;
  }

  function qaBadge(report) {
    if (!report) return '';
    const level = report.score >= 90 ? 'good' : report.score >= 75 ? 'review' : 'bad';
    return `<span class="badge reader-qa-badge qa-${level}">QA ${report.score}</span>`;
  }
  async function loadChapterContent(slug, chapterNo) {
    cancelPendingLoad();
    const id = requestID;
    const isCurrent = () => id === requestID && state.currentView === 'reader'
      && state.currentSlug === slug && state.currentChapterNo === chapterNo;
    chapterRequest = new AbortController();
    state.currentChapterData = null;
    el.readerChapterTitle.textContent = `ตอนที่ ${chapterNo}`;
    el.readerContent.setAttribute('aria-busy', 'true');
    el.readerContent.innerHTML = '<div class="reader-state">กำลังโหลด...</div>';
    updateNavigationState(chapterNo);
    try {
      const chapter = await api(`/api/novels/${encodeURIComponent(slug)}/chapters/${chapterNo}`, { signal: chapterRequest.signal, silent: true });
      if (!isCurrent()) return false;
      state.currentChapterData = chapter;
      const novel = state.currentNovel || { title: slug };
      el.readerNovelTitle.textContent = novel.translatedTitle || novel.title || slug;
      el.readerNovelTitle.classList.add('reader-novel-link');
      el.readerNovelTitle.title = 'กลับไปหน้ารายละเอียดเรื่อง';

      const title = chapter.translatedTitle || chapter.sourceTitle || `ตอนที่ ${chapterNo}`;
      const qa = (state.qaReports || []).find(report => report.chapterNo === chapterNo);
      el.readerChapterTitle.innerHTML = `${escapeHTML(title)} ${qaBadge(qa)}`;
      renderReaderParagraphs();
      updateNavigationState(chapterNo);
      return true;
    } catch (err) {
      if (!isCurrent() || err.name === 'AbortError') return false;
      el.readerContent.innerHTML = `<div class="reader-state reader-state-error">โหลดตอนที่ ${chapterNo} ไม่สำเร็จ <button class="btn btn-outline" type="button" data-action="retry-current">ลองอีกครั้ง</button></div>`;
      console.error('load chapter failed', err);
      return false;
    } finally {
      if (id === requestID) el.readerContent.removeAttribute('aria-busy');
    }
  }

  function updateNavigationState(chapterNo) {
    const index = (state.chapters || []).findIndex(chapter => chapter.chapterNo === chapterNo);
    const hasPrev = adjacentChapterNo ? adjacentChapterNo(chapterNo, -1) !== null : index >= 0 ? index > 0 : chapterNo > 1;
    const hasNext = adjacentChapterNo ? adjacentChapterNo(chapterNo, 1) !== null : index >= 0 ? index < state.chapters.length - 1 : chapterNo < maxChapterNo();
    el.btnPrevChapter.disabled = !hasPrev;
    el.btnNextChapter.disabled = !hasNext;
    if (el.btnPrevChapterTop) el.btnPrevChapterTop.disabled = !hasPrev;
    if (el.btnNextChapterTop) el.btnNextChapterTop.disabled = !hasNext;
  }

  function renderReaderParagraphs() {
    const chapter = state.currentChapterData;
    if (!chapter) return;
    const translated = chapter.translatedText || [];
    const source = chapter.sourceText || [];
    const hasTranslation = translated.length > 0;
    const blocks = [];

    if (!hasTranslation) {
      blocks.push(`
        <div class="reader-untranslated">
          <p>ตอนนี้ยังไม่ได้แปล (แสดงภาษาต้นฉบับ)</p>
          <button class="btn btn-primary btn-sm" type="button" data-action="translate-current">⚡ สั่งแปลตอนนี้</button>
        </div>`);
    }

    if (state.readingMode === 'bilingual' && hasTranslation && source.length) {
      const maxLength = Math.max(translated.length, source.length);
      for (let index = 0; index < maxLength; index++) {
        const thai = translated[index] || '';
        const original = source[index] || '';
        blocks.push(`
          <div class="bilingual-pair">
            ${thai ? `<p class="para-th">${escapeHTML(thai)}</p>` : ''}
            ${original ? `<p class="para-src">${escapeHTML(original)}</p>` : ''}
          </div>`);
      }
    } else if (state.readingMode === 'source' && source.length) {
      blocks.push(source.map(paragraph => `<p>${escapeHTML(paragraph)}</p>`).join(''));
    } else {
      const paragraphs = hasTranslation ? translated : source;
      blocks.push(paragraphs.map((paragraph, index) =>
        `<p class="reader-p" data-p-idx="${index}">${escapeHTML(paragraph)}</p>`).join(''));
    }

    el.readerContent.innerHTML = blocks.join('');
    restoreScrollPosition();
  }

  function restoreScrollPosition() {
    clearTimeout(restoreTimer);
    const id = requestID;
    const saved = Number.parseInt(localStorage.getItem(`nc_scroll_${state.currentSlug}_${state.currentChapterNo}`), 10);
    restoreTimer = window.setTimeout(() => {
      if (id === requestID && state.currentView === 'reader') {
        window.scrollTo({ top: Number.isFinite(saved) ? Math.max(0, saved) : 0, behavior: 'auto' });
      }
    }, 80);
  }
  function getModeLabel(mode) {
    if (mode === 'bilingual') return '2 ภาษา';
    if (mode === 'source') return 'ต้นฉบับ';
    return 'ไทย';
  }

  function updateModeButtonText() {
    if (el.btnReaderMode) el.btnReaderMode.textContent = `🌐 ${getModeLabel(state.readingMode)}`;
  }

  function cycleReadingMode() {
    saveReadingPosition();
    state.readingMode = state.readingMode === 'thai'
      ? 'bilingual'
      : state.readingMode === 'bilingual' ? 'source' : 'thai';
    localStorage.setItem('nc_reading_mode', state.readingMode);
    updateModeButtonText();
    renderReaderParagraphs();
    showToast(`โหมดอ่าน: ${getModeLabel(state.readingMode)}`, 'info');
  }

  function bindReaderCoreEvents() {
    el.readerNovelTitle?.addEventListener('click', () => {
      if (state.currentSlug) openNovelDetail(state.currentSlug);
    });
    el.readerNovelTitle?.addEventListener('keydown', event => {
      if (event.key !== 'Enter' && event.key !== ' ') return;
      event.preventDefault();
      if (state.currentSlug) openNovelDetail(state.currentSlug);
    });
    el.readerContent?.addEventListener('click', event => {
      if (event.target.closest('[data-action="retry-current"]')) {
        openChapter(state.currentSlug, state.currentChapterNo);
        return;
      }
      if (!event.target.closest('[data-action="translate-current"]')) return;
      triggerQuickTranslate(state.currentSlug, state.currentChapterNo, state.currentChapterNo);
    });
  }

  return {
    openChapter,
    loadChapterContent,
    renderReaderParagraphs,
    cycleReadingMode,
    updateModeButtonText,
    getModeLabel,
    bindReaderCoreEvents,
    cancelPendingLoad,
  };
}
