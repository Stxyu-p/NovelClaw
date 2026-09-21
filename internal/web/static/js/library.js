import { escapeHTML } from './utils.js';

export function createLibraryController({
  state, el, api, showView, openChapter, openImportModal, formatGenre, showToast,
}) {
  let detailRequest = 0;
  let chapterRequest = 0;
  let searchTimer = null;
  let libraryTimer = null;
  let libraryVersion = 0;
  // Hidden file picker behind the "เปลี่ยนปก" button on the detail hero.
  const coverPicker = document.createElement('input');
  coverPicker.type = 'file';
  coverPicker.accept = 'image/webp,image/jpeg,image/png';
  coverPicker.hidden = true;
  document.body.appendChild(coverPicker);
  coverPicker.addEventListener('change', () => {
    const file = coverPicker.files?.[0];
    coverPicker.value = '';
    if (file) changeCover(file);
  });

  async function changeCover(file) {
    if (!state.currentSlug) return;
    if (file.size > 4 * 1024 * 1024) {
      showToast('ไฟล์ปกใหญ่เกิน 4 MB', 'error');
      return;
    }
    try {
      await api(`/api/novels/${state.currentSlug}/cover`, {
        method: 'POST',
        body: file,
        headers: { 'Content-Type': file.type || 'application/octet-stream' },
      });
      state.coverVersion += 1;
      showToast('เปลี่ยนหน้าปกเรียบร้อยแล้ว', 'success');
      await openNovelDetail(state.currentSlug);
    } catch (err) {
      showToast(`เปลี่ยนหน้าปกไม่สำเร็จ: ${err.message}`, 'error');
    }
  }

  function coverURLFor(slug) {
    const base = `/api/novels/${encodeURIComponent(slug)}/cover`;
    return state.coverVersion ? `${base}?v=${state.coverVersion}` : base;
  }

  function renderEmptyLibrary() {
    el.novelGrid.innerHTML = `
      <div class="library-empty">
        <div class="library-empty-icon">📖</div>
        <p>ยังไม่มีนิยายในคลัง</p>
        <button class="btn btn-primary" type="button" data-action="import-first">📥 นำเข้านิยายเรื่องแรก</button>
      </div>`;
  }

  function renderNovelCard(novel) {
    const total = novel.totalChapters || 0;
    const translated = novel.translatedChapters || 0;
    const pct = total ? Math.round((translated / total) * 100) : 0;
    const displayTitle = novel.translatedTitle || novel.title || 'Untitled';
    const genreIcons = { apocalypse: '❄️', xianxia: '🥋', system: '🎮', fantasy: '✨', urban: '🏙️', scifi: '🌌', historical: '🏯', horror: '👻', romance: '💗' };
    const genreIcon = genreIcons[novel.genre] || '📖';
    const genreBadge = novel.genre ? `<span class="novel-genre">${escapeHTML(formatGenre(novel.genre))}</span>` : '';
    const coverURL = coverURLFor(novel.slug);
    const cover = `<div class="novel-cover-fallback" data-genre="${escapeHTML(novel.genre || 'novel')}"><span>${genreIcon}</span><strong>${escapeHTML(displayTitle.slice(0, 1).toUpperCase())}</strong></div><img class="novel-cover-image" src="${coverURL}" alt="ปก ${escapeHTML(displayTitle)}" loading="lazy">`;
    return `
      <article class="novel-card" data-slug="${escapeHTML(novel.slug)}" tabindex="0" role="button">
        <div class="novel-cover-shell">
          ${cover}
          <span class="novel-cover-progress">${pct}%</span>
        </div>
        <div class="novel-card-body">
          <div class="novel-card-heading">
            ${genreBadge}
            <h3 class="novel-card-title">${escapeHTML(displayTitle)}</h3>
            <p class="novel-card-subtitle">${escapeHTML(novel.description || novel.title || 'ไม่มีคำอธิบาย')}</p>
          </div>
          <div class="novel-card-stats">
            <span><strong>${total}</strong><small>ตอน</small></span>
            <span><strong>${translated}</strong><small>แปลแล้ว</small></span>
            <span><strong>${pct}%</strong><small>ความคืบหน้า</small></span>
          </div>
          <div class="novel-progress" aria-label="แปลแล้ว ${pct}%"><span style="width:${pct}%"></span></div>
          <div class="novel-card-footer">
            <span class="novel-card-author">${escapeHTML(novel.author || 'ไม่ระบุผู้แต่ง')}</span>
            <span class="novel-card-read">เปิดเรื่อง <span aria-hidden="true">→</span></span>
          </div>
        </div>
      </article>`;
  }
  function renderLibrary() {
    if (!state.novels.length) { renderEmptyLibrary(); return; }
    const query = (document.getElementById('library-search')?.value || '').trim().toLocaleLowerCase();
    const order = document.getElementById('library-sort')?.value || 'recent';
    const novels = state.novels.filter(novel => `${novel.title || ''} ${novel.translatedTitle || ''} ${novel.author || ''}`.toLocaleLowerCase().includes(query));
    novels.sort((a,b) => order === 'title' ? (a.translatedTitle || a.title || '').localeCompare(b.translatedTitle || b.title || '', 'th') : order === 'translated' ? (b.translatedChapters || 0) - (a.translatedChapters || 0) : (Date.parse(b.updatedAt) || 0) - (Date.parse(a.updatedAt) || 0));
    el.novelCount.textContent = `${novels.length} / ${state.novels.length} เรื่อง`;
    el.novelGrid.innerHTML = novels.length ? novels.map(renderNovelCard).join('') : '<div class="library-empty"><h2>ไม่พบเรื่องที่ค้นหา</h2><p>ลองใช้ชื่อสั้นลง หรือค้นหาจากผู้แต่ง</p><button class="btn btn-outline" data-action="clear-search">ล้างการค้นหา</button></div>';
  }
  async function loadNovels() {
    const version = ++libraryVersion;
    try {
      const res = await api('/api/novels');
      if (version !== libraryVersion) return;
      state.novels = res.novels || [];
      el.novelCount.textContent = `${state.novels.length} เรื่อง`;
      if (state.novels.length === 0) {
        renderEmptyLibrary();
        return;
      }
      renderLibrary();
    } catch (err) {
      showToast('โหลดรายการนิยายไม่สำเร็จ: ' + (err?.message || err), 'error');
      console.error('loadNovels failed', err);
    }
  }

  function resetChapterBrowser() {
    state.chapterPage = 1;
    state.chapterQuery = '';
    state.chapterFilter = 'all';
    if (el.chapterSearch) el.chapterSearch.value = '';
    if (el.chapterFilter) el.chapterFilter.value = 'all';
  }
  async function openNovelDetail(slug) {
    const request = ++detailRequest;
    const switchingNovel = state.currentSlug !== slug;
    state.currentSlug = slug;
    if (switchingNovel) resetChapterBrowser();
    if (switchingNovel) {
      state.currentNovel = null;
      state.chapters = [];
      state.qaReports = [];
    }
    showView('detail');
    el.detailHeader.innerHTML = '<div class="reader-state" role="status">กำลังเปิดเรื่อง...</div>';
    el.chapterList.innerHTML = '<div class="reader-state" role="status">กำลังโหลดสารบัญ...</div>';

    try {
      const [novel, bookmark] = await Promise.all([
        api(`/api/novels/${encodeURIComponent(slug)}`),
        api(`/api/novels/${encodeURIComponent(slug)}/bookmark`, { silent: true }).catch(() => ({ chapterNo: 1 })),
      ]);
      if (request !== detailRequest || state.currentSlug !== slug || state.currentView !== 'detail') return;
      state.currentNovel = novel;
      const latestCh = bookmark.chapterNo || 1;
      renderNovelDetailHeader(novel, latestCh);
      if (novel.genre && el.transGenre) el.transGenre.value = novel.genre;
      await loadChapters(slug);
    } catch (err) {
      if (request !== detailRequest || state.currentSlug !== slug || state.currentView !== 'detail') return;
      el.detailHeader.innerHTML = '<div class="reader-state reader-state-error">เปิดเรื่องไม่สำเร็จ กรุณากลับคลังแล้วลองอีกครั้ง</div>';
      showToast('เปิดเรื่องไม่สำเร็จ: ' + (err?.message || err), 'error');
      console.error('openNovelDetail failed', err);
    }
  }
  function renderNovelDetailHeader(novel, latestCh) {
    const displayTitle = novel.translatedTitle || novel.title || 'Untitled';
    const total = novel.totalChapters || 0;
    const translated = novel.translatedChapters || 0;
    const pct = total ? Math.round((translated / total) * 100) : 0;
    const genreIcons = { apocalypse: '❄️', xianxia: '🥋', system: '🎮', fantasy: '✨', urban: '🏙️', scifi: '🌌', historical: '🏯', horror: '👻', romance: '💗' };
    const genreIcon = genreIcons[novel.genre] || '📖';
    const coverURL = `/api/novels/${encodeURIComponent(novel.slug)}/cover`;
    const cover = `<div class="novel-detail-cover-fallback"><span>${genreIcon}</span><strong>${escapeHTML(displayTitle.slice(0, 1).toUpperCase())}</strong></div><img class="novel-detail-cover-image" src="${coverURLFor(novel.slug)}" alt="ปก ${escapeHTML(displayTitle)}">`;
    const genreBadge = novel.genre ? `<span class="novel-detail-genre">${escapeHTML(formatGenre(novel.genre))}</span>` : '';
    el.detailHeader.innerHTML = `
      <div class="novel-detail-hero">
        <div class="novel-detail-cover">${cover}</div>
        <div class="novel-detail-copy">
          <div class="novel-detail-kicker">${genreBadge}<span>NovelClaw Library</span></div>
          <h1>${escapeHTML(displayTitle)}</h1>
          <p class="novel-detail-original">${escapeHTML(novel.title || '')}</p>
          <p class="novel-detail-author">โดย ${escapeHTML(novel.author || 'ไม่ระบุผู้แต่ง')}</p>
          <div class="novel-detail-stats">
            <span><strong>${total}</strong><small>ตอนทั้งหมด</small></span>
            <span><strong>${translated}</strong><small>แปลแล้ว</small></span>
            <span><strong>${pct}%</strong><small>ความคืบหน้า</small></span>
          </div>
          <p class="novel-detail-description">${escapeHTML(novel.description || 'ยังไม่มีคำอธิบายเรื่อง')}</p>
          <div class="novel-detail-actions">
            <button class="btn btn-primary btn-lg" type="button" data-continue-ch="${latestCh}">อ่านต่อ ตอนที่ ${latestCh} <span aria-hidden="true">→</span></button>
            <button class="btn btn-outline btn-lg" type="button" data-change-cover>🖼️ เปลี่ยนปก</button>
          </div>
        </div>
      </div>`;
  }
  async function loadChapters(slug) {
    if (slug !== state.currentSlug) return;
    const request = ++chapterRequest;
    try {
      const [chapterRes, qaRes] = await Promise.all([
        api(`/api/novels/${encodeURIComponent(slug)}/chapters`),
        api(`/api/novels/${encodeURIComponent(slug)}/qa`, { silent: true }).catch(() => null),
      ]);
      if (request !== chapterRequest || state.currentSlug !== slug) return;
      state.chapters = chapterRes.chapters || [];
      if (qaRes) state.qaReports = qaRes.reports || [];
      if (state.currentView === 'detail') renderChapterList(slug);
    } catch (err) {
      if (request !== chapterRequest || state.currentSlug !== slug) return;
      showToast('โหลดรายการตอนไม่สำเร็จ: ' + (err?.message || err), 'error');
      console.error('loadChapters failed', err);
    }
  }

  function filterChapters() {
    const qaByChapter = new Map((state.qaReports || []).map(report => [report.chapterNo, report]));
    const query = (state.chapterQuery || '').trim().toLowerCase();
    const filter = state.chapterFilter || 'all';
    const chapters = (state.chapters || []).filter(chapter => {
      const qa = qaByChapter.get(chapter.chapterNo);
      if (filter === 'translated' && !chapter.hasTranslated) return false;
      if (filter === 'source' && chapter.hasTranslated) return false;
      if (filter === 'qa-review' && (!qa || qa.score >= 90)) return false;
      if (filter === 'qa-bad' && (!qa || qa.score >= 75)) return false;
      if (!query) return true;
      const haystack = `${chapter.chapterNo} ${chapter.titleSource || ''} ${chapter.titleTranslated || ''}`.toLowerCase();
      return haystack.includes(query);
    });
    return { chapters, qaByChapter };
  }

  function renderChapterList() {
    const { chapters: filtered, qaByChapter } = filterChapters();
    const pageSize = state.chapterPageSize;
    const pageCount = Math.max(1, Math.ceil(filtered.length / pageSize));
    state.chapterPage = Math.max(1, Math.min(state.chapterPage, pageCount));
    const start = (state.chapterPage - 1) * pageSize;
    const visible = filtered.slice(start, start + pageSize);

    el.chapterList.innerHTML = visible.length
      ? visible.map(chapter => renderChapterRow(chapter, qaByChapter.get(chapter.chapterNo))).join('')
      : '<div class="chapter-empty">ไม่พบตอนที่ตรงกับเงื่อนไข</div>';
    const shownFrom = filtered.length === 0 ? 0 : start + 1;
    const shownTo = Math.min(start + visible.length, filtered.length);
    el.chapterPageInfo.textContent = `${shownFrom}-${shownTo} จาก ${filtered.length} ตอน • หน้า ${state.chapterPage}/${pageCount}`;
    el.btnChapterPagePrev.disabled = state.chapterPage <= 1;
    el.btnChapterPageNext.disabled = state.chapterPage >= pageCount;
  }

  function renderChapterRow(chapter, qa) {
    const titleCandidate = (chapter.titleTranslated || '').trim();
    const titleText = titleCandidate && !/^ตอนที่\s*\d+$/.test(titleCandidate)
      ? chapter.titleTranslated
      : (chapter.titleSource || '');
    const qaClass = qa ? (qa.score >= 90 ? 'qa-good' : qa.score >= 75 ? 'qa-review' : 'qa-bad') : '';
    const locked = Boolean(chapter.locked && !chapter.hasSource && !chapter.hasTranslated);
    const status = locked ? '🔒 VIP' : (chapter.hasTranslated ? 'แปลแล้ว' : 'ต้นฉบับ');
    const badgeClass = locked ? 'badge-muted' : (chapter.hasTranslated ? 'badge-success' : 'badge-info');
    return `
      <button type="button" class="chapter-item ${chapter.hasTranslated ? 'translated' : ''} ${locked ? 'locked' : ''}" data-ch="${chapter.chapterNo}" ${locked ? 'disabled aria-disabled="true"' : ''}>
        <span class="chapter-number">ตอนที่ ${chapter.chapterNo}</span>
        <span class="chapter-title-text">${escapeHTML(titleText)}</span>
        <span class="chapter-item-spacer"></span>
        ${qa ? `<span class="badge qa-badge ${qaClass}">QA ${qa.score}</span>` : ''}
        <span class="badge ${badgeClass}">${status}</span>
      </button>`;
  }
  function adjacentChapterNo(chapterNo, direction) {
    if (!state.chapters || state.chapters.length === 0) {
      const candidate = chapterNo + direction;
      return candidate >= 1 ? candidate : null;
    }
    const readable = state.chapters.filter(chapter => chapter.hasSource || chapter.hasTranslated);
    const index = readable.findIndex(chapter => chapter.chapterNo === chapterNo);
    if (index === -1) {
      const candidates = readable.filter(chapter => direction > 0 ? chapter.chapterNo > chapterNo : chapter.chapterNo < chapterNo);
      return direction > 0 ? candidates[0]?.chapterNo ?? null : candidates[candidates.length - 1]?.chapterNo ?? null;
    }
    return readable[index + direction]?.chapterNo ?? null;
  }

  function maxChapterNo() {
    if (!state.chapters || state.chapters.length === 0) return 1;
    return state.chapters[state.chapters.length - 1].chapterNo;
  }

  function activateNovelCard(target) {
    const card = target.closest?.('.novel-card[data-slug]');
    if (card?.dataset.slug) openNovelDetail(card.dataset.slug);
  }
  function bindLibraryEvents() {
    document.getElementById('library-search')?.addEventListener('input', () => { clearTimeout(libraryTimer); libraryTimer = setTimeout(renderLibrary, 120); });
    document.getElementById('library-sort')?.addEventListener('change', renderLibrary);
    const hideBrokenCover = event => {
      const image = event.target?.closest?.('.novel-cover-image, .novel-detail-cover-image');
      if (image) image.classList.add('is-missing');
    };
    el.novelGrid?.addEventListener('error', hideBrokenCover, true);
    el.detailHeader?.addEventListener('error', hideBrokenCover, true);
    el.novelGrid?.addEventListener('click', event => {
      if (event.target.closest('[data-action="import-first"]')) {
        openImportModal();
        return;
      }
      if (event.target.closest('[data-action="clear-search"]')) { document.getElementById('library-search').value = ''; renderLibrary(); return; }
      activateNovelCard(event.target);
    });
    el.novelGrid?.addEventListener('keydown', event => {
      if (event.target.closest('button, input, select, a')) return;
      if (event.key === 'Enter' || event.key === ' ') {
        event.preventDefault();
        activateNovelCard(event.target);
      }
    });
    el.detailHeader?.addEventListener('click', event => {
      const button = event.target.closest('[data-continue-ch]');
      if (!button || !state.currentSlug) return;
      const chapterNo = Number.parseInt(button.dataset.continueCh, 10);
      if (chapterNo > 0) openChapter(state.currentSlug, chapterNo);
    });
    el.detailHeader?.addEventListener('click', event => {
      if (event.target.closest('[data-change-cover]')) coverPicker.click();
    });
    el.chapterSearch?.addEventListener('input', () => {
      state.chapterQuery = el.chapterSearch.value;
      state.chapterPage = 1;
      clearTimeout(searchTimer);
      searchTimer = setTimeout(() => { if (state.currentView === 'detail') renderChapterList(); }, 120);
    });
    el.chapterFilter?.addEventListener('change', () => {
      state.chapterFilter = el.chapterFilter.value;
      state.chapterPage = 1;
      renderChapterList();
    });
    el.btnChapterPagePrev?.addEventListener('click', () => {
      if (state.chapterPage <= 1) return;
      state.chapterPage -= 1;
      renderChapterList();
    });
    el.btnChapterPageNext?.addEventListener('click', () => {
      state.chapterPage += 1;
      renderChapterList();
    });
    el.chapterList?.addEventListener('click', event => {
      const item = event.target.closest('[data-ch]');
      if (!item || !state.currentSlug) return;
      const chapterNo = Number.parseInt(item.dataset.ch, 10);
      if (chapterNo > 0) openChapter(state.currentSlug, chapterNo);
    });
  }
  return {
    loadNovels,
    openNovelDetail,
    loadChapters,
    renderChapterList,
    adjacentChapterNo,
    maxChapterNo,
    bindLibraryEvents,
  };
}
