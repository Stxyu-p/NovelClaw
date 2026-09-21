import { escapeHTML } from './utils.js';

export function createGlossaryController({
  state, el, api, showToast, openModal, closeModal,
}) {
  let session = 0;
  let loaded = false;
  let editVersion = 0;
  const isCurrent = (id, slug) => id === session && state.currentSlug === slug && !el.modalGlossary.classList.contains('hidden');
  async function openGlossary() {
    if (!state.currentSlug) return;
    const id = ++session, slug = state.currentSlug;
    loaded = false;
    state.glossaryTerms = [];
    el.btnSaveGlossary.disabled = true;
    openModal(el.modalGlossary);
    el.discStatus.textContent = '';
    el.glossaryQaResults.innerHTML = '';
    el.glossaryTbody.innerHTML = '<tr><td colspan="4" class="table-empty">กำลังโหลดคำศัพท์...</td></tr>';
    try {
      const res = await api(`/api/novels/${encodeURIComponent(slug)}/glossary`);
      if (!isCurrent(id, slug)) return;
      state.glossaryTerms = res.terms || [];
      loaded = true;
      el.btnSaveGlossary.disabled = false;
      renderGlossaryTable();
    } catch (err) {
      if (!isCurrent(id, slug)) return;
      el.glossaryTbody.innerHTML = '<tr><td colspan="4" class="table-empty">โหลดคำศัพท์ไม่สำเร็จ กรุณาปิดแล้วเปิดใหม่</td></tr>';
      console.error('load glossary failed', err);
    }
  }

  function renderGlossaryTable() {
    if (!state.glossaryTerms.length) {
      el.glossaryTbody.innerHTML = '<tr><td colspan="4" class="table-empty">ยังไม่มีคำศัพท์</td></tr>';
      return;
    }
    // Every cell is an editable input — fix a wrong AI translation in place,
    // then hit "บันทึก Glossary" to persist the whole table.
    // innerHTML is safe here: term/target/category all pass escapeHTML, and
    // the <option> list is hardcoded literals (same pattern as the codebase).
    el.glossaryTbody.innerHTML = state.glossaryTerms.map((term, index) => `
      <tr class="glossary-row">
        <td><input class="form-input glossary-edit glossary-edit-orig" data-idx="${index}" data-field="term" value="${escapeHTML(term.term)}"></td>
        <td><input class="form-input glossary-edit" data-idx="${index}" data-field="target" value="${escapeHTML(term.target)}"></td>
        <td>
          <select class="form-input glossary-edit" data-idx="${index}" data-field="category">
            ${['', 'character', 'location', 'skill', 'item', 'custom'].map(cat => `<option value="${cat}" ${term.category === cat ? 'selected' : ''}>${cat || '— ไม่ระบุ —'}</option>`).join('')}
          </select>
        </td>
        <td class="glossary-actions">
          <button type="button" class="btn btn-outline btn-sm btn-remove-term" data-idx="${index}">✕</button>
        </td>
      </tr>`).join('');
  }

  function onGlossaryEdit(event) {
    const input = event.target.closest('.glossary-edit');
    if (!input) return;
    const index = Number.parseInt(input.dataset.idx, 10);
    const field = input.dataset.field;
    if (!Number.isInteger(index) || index < 0 || index >= state.glossaryTerms.length || !field) return;
    state.glossaryTerms[index][field] = input.value.trim();
    editVersion++;
  }

  async function discoverGlossary() {
    if (!state.currentSlug || !loaded || el.btnRunDiscovery.disabled) return;
    const id = session, slug = state.currentSlug, edits = editVersion;
    el.btnRunDiscovery.disabled = true;
    const start = Number.parseInt(el.discStart.value, 10) || 1;
    const end = Number.parseInt(el.discEnd.value, 10) || start;
    const model = el.transModelSelect.value || state.defaultModel;
    el.discStatus.textContent = '⏳ กำลังสแกนชื่อตัวละคร...';
    try {
      const res = await api(`/api/novels/${state.currentSlug}/glossary/discover`, {
        method: 'POST',
        body: JSON.stringify({ novelSlug: state.currentSlug, startChapter: start, endChapter: end, model }),
      });
      if (!isCurrent(id, slug)) return;
      if (edits !== editVersion) {
        el.discStatus.textContent = 'มีการแก้ไขศัพท์ระหว่างสแกน จึงเก็บการแก้ไขของคุณไว้ กรุณาบันทึกก่อนสแกนใหม่';
        return;
      }
      const found = res.discovered?.length || 0;
      const added = res.added?.length || 0;
      const skipped = res.skippedBuiltin || 0;
      const dupes = found - added - skipped;
      state.glossaryTerms = res.glossary?.terms || [];
      renderGlossaryTable();
      el.discStatus.textContent = added
        ? `✅ เพิ่มศัพท์ใหม่ ${added} คำ (โมเดลเจอ ${found} — ซ้ำในตาราง ${dupes}, builtin ล็อกอยู่แล้ว ${skipped}) — กด "บันทึก Glossary" เพื่อเก็บถาวร`
        : `✅ สแกนสำเร็จแต่ไม่มีศัพท์ใหม่ (โมเดลเจอ ${found} คำ — ซ้ำในตาราง ${dupes}, builtin ล็อกอยู่แล้ว ${skipped})`;
      showToast(added ? `เพิ่มศัพท์ใหม่ ${added} คำ (ยังไม่บันทึก)` : `ไม่มีศัพท์ใหม่ — ที่เจอเป็นศัพท์ซ้ำ/builtin ทั้งหมด`, 'info');
    } catch (err) {
      if (!isCurrent(id, slug)) return;
      el.discStatus.textContent = '❌ สแกนล้มเหลว';
      showToast(`การสแกนศัพท์ล้มเหลว: ${err.message}`, 'error');
    } finally {
      el.btnRunDiscovery.disabled = false;
    }
  }

  function addManualTerm(event) {
    event.preventDefault();
    if (!loaded) return;
    const term = el.termOrig?.value.trim() || '';
    const target = el.termTarget?.value.trim() || '';
    const category = el.termCategory?.value || '';
    if (!term || !target) return;
    state.glossaryTerms.push({ term, target, category });
    editVersion++;
    renderGlossaryTable();
    el.termOrig.value = '';
    el.termTarget.value = '';
  }
  async function saveGlossary() {
    if (!state.currentSlug || !loaded || el.btnSaveGlossary.disabled) return;
    const id = session, slug = state.currentSlug;
    el.btnSaveGlossary.disabled = true;
    try {
      await api(`/api/novels/${state.currentSlug}/glossary`, {
        method: 'POST',
        body: JSON.stringify({ novelSlug: state.currentSlug, terms: state.glossaryTerms }),
      });
      if (!isCurrent(id, slug)) return;
      showToast('บันทึก Glossary เรียบร้อยแล้ว', 'success');
      closeModal(el.modalGlossary);
    } catch (err) {
      console.error('save glossary failed', err);
    } finally {
      if (id === session) el.btnSaveGlossary.disabled = !loaded;
    }
  }

  function renderGlossaryQA(res) {
    const issues = res.issues || [];
    if (!issues.length) {
      el.glossaryQaResults.innerHTML = `<div class="glossary-qa-ok">✅ ตรวจ ${res.scanned} ตอน — ศัพท์สอดคล้องทั้งหมด</div>`;
      return;
    }
    const byChapter = new Map();
    for (const issue of issues) {
      if (!byChapter.has(issue.chapterNo)) byChapter.set(issue.chapterNo, []);
      byChapter.get(issue.chapterNo).push(issue);
    }
    el.glossaryQaResults.innerHTML = `
      <div class="glossary-qa-warning">⚠️ พบ ${issues.length} จุดใน ${byChapter.size} ตอน (สแกน ${res.scanned} ตอน)</div>` +
      Array.from(byChapter.entries()).map(([chapterNo, list]) => `
        <div class="glossary-qa-row">
          <span class="glossary-qa-chapter">ตอนที่ ${chapterNo}</span>
          <span class="glossary-qa-terms">
            ${list.map(issue => `${escapeHTML(issue.term)} → ${escapeHTML(issue.expected)}`).join(', ')}
          </span>
          <button type="button" class="btn btn-outline btn-sm btn-qa-repair" data-ch="${chapterNo}">🔧 ซ่อม</button>
        </div>`).join('');
  }

  async function runGlossaryQA() {
    if (!state.currentSlug) return;
    const id = session, slug = state.currentSlug;
    const start = Number.parseInt(el.qaStart?.value, 10) || 1;
    const end = Number.parseInt(el.qaEnd?.value, 10) || start;
    el.btnGlossaryCheck.disabled = true;
    el.btnGlossaryCheck.textContent = '⏳ กำลังตรวจ...';
    try {
      const res = await api(`/api/novels/${state.currentSlug}/glossary/check?start=${start}&end=${end}`);
      if (!isCurrent(id, slug)) return;
      renderGlossaryQA(res);
      el.glossaryQaResults.style.display = 'block';
    } catch (err) {
      showToast(`การตรวจสอบล้มเหลว: ${err.message}`, 'error');
    } finally {
      el.btnGlossaryCheck.disabled = false;
      el.btnGlossaryCheck.textContent = '🔍 ตรวจสอบ';
    }
  }

  async function repairChapter(button) {
    if (!state.currentSlug || !button?.dataset.ch) return;
    const chapterNo = button.dataset.ch;
    button.disabled = true;
    button.textContent = '⏳';
    try {
      await api(`/api/novels/${state.currentSlug}/chapters/${chapterNo}/repair`, { method: 'POST' });
      showToast(`ซ่อมตอนที่ ${chapterNo} เรียบร้อย`, 'success');
      button.textContent = '✅';
    } catch (err) {
      showToast(`ซ่อมตอนที่ ${chapterNo} ล้มเหลว: ${err.message}`, 'error');
      button.textContent = '❌';
      button.disabled = false;
    }
  }
  function bindGlossaryEvents() {
    el.btnOpenGlossary?.addEventListener('click', openGlossary);
    el.btnCloseGlossary?.addEventListener('click', () => closeModal(el.modalGlossary));
    el.btnRunDiscovery?.addEventListener('click', discoverGlossary);
    el.formAddTerm?.addEventListener('submit', addManualTerm);
    el.btnSaveGlossary?.addEventListener('click', saveGlossary);
    el.btnGlossaryCheck?.addEventListener('click', runGlossaryQA);
    el.glossaryTbody?.addEventListener('click', event => {
      const button = event.target.closest('.btn-remove-term');
      if (!button) return;
      const index = Number.parseInt(button.dataset.idx, 10);
      if (Number.isInteger(index) && index >= 0) {
        state.glossaryTerms.splice(index, 1);
        editVersion++;
        renderGlossaryTable();
      }
    });
    el.glossaryTbody?.addEventListener('change', onGlossaryEdit);
    el.glossaryTbody?.addEventListener('input', event => {
      // Text inputs update state live so a click on บันทึก always has fresh values.
      if (event.target.classList.contains('glossary-edit') && event.target.tagName === 'INPUT') onGlossaryEdit(event);
    });
    el.glossaryQaResults?.addEventListener('click', event => {
      const button = event.target.closest('.btn-qa-repair');
      if (button) repairChapter(button);
    });
  }

  return { bindGlossaryEvents, openGlossary, renderGlossaryTable };
}
