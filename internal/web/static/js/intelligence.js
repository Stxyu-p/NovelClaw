import { escapeHTML } from './utils.js';

export function createIntelligenceController({
  state, el, api, showToast, openModal, closeModal, loadChapters,
}) {
  let session = 0;
  let loaded = false;
  let editVersion = 0;
  const isCurrent = (id, slug) => id === session && state.currentSlug === slug && !el.modalIntelligence.classList.contains('hidden');
  function applyMemoryToForm(memory) {
    state.novelMemory = {
      storySummary: memory?.storySummary || '',
      characters: memory?.characters || [],
      facts: memory?.facts || [],
    };
    el.memorySummary.value = state.novelMemory.storySummary;
    el.memoryFacts.value = state.novelMemory.facts.join('\n');
    renderMemoryCharacters();
  }

  function setDefaultMemoryRange() {
    const available = (state.chapters || [])
      .filter(ch => ch.hasSource || ch.hasTranslated)
      .map(ch => Number(ch.chapterNo))
      .filter(Number.isFinite)
      .sort((a, b) => a - b);
    if (!available.length) return;
    const recent = available.slice(-5);
    el.memoryAiStart.value = recent[0];
    el.memoryAiEnd.value = recent[recent.length - 1];
  }

  async function openIntelligenceModal() {
    if (!state.currentSlug) return;
    const id = ++session, slug = state.currentSlug;
    loaded = false;
    openModal(el.modalIntelligence);
    el.memorySummary.value = '';
    el.memoryFacts.value = '';
    el.memoryCharacters.innerHTML = '';
    el.qaSummary.textContent = 'กำลังโหลดข้อมูล...';
    el.qaScoreStrip.innerHTML = '';
    el.qaRepairList.innerHTML = '';
    try {
      const [memory, qaRes] = await Promise.all([
        api(`/api/novels/${state.currentSlug}/memory`),
        api(`/api/novels/${state.currentSlug}/qa`),
      ]);
      if (!isCurrent(id, slug)) return;
      loaded = true;
      applyMemoryToForm(memory);
      state.qaReports = qaRes.reports || [];
      setDefaultMemoryRange();
      renderQASummary();
    } catch (err) {
      if (!isCurrent(id, slug)) return;
      el.qaSummary.textContent = 'โหลดข้อมูลไม่สำเร็จ กรุณาปิดแล้วเปิดใหม่';
      console.error('load intelligence failed', err);
    }
  }
  function renderQASummary() {
    const reports = state.qaReports || [];
    if (reports.length === 0) {
      el.qaSummary.textContent = 'ยังไม่มีผลตรวจ — กดสแกน QA ย้อนหลังเพื่อสร้างคะแนน';
      el.qaScoreStrip.innerHTML = '';
      el.qaRepairList.innerHTML = '';
      return;
    }
    const avg = Math.round(reports.reduce((sum, report) => sum + (report.score || 0), 0) / reports.length);
    const good = reports.filter(report => report.score >= 90).length;
    const review = reports.filter(report => report.score >= 75 && report.score < 90).length;
    const poor = reports.filter(report => report.score < 75).length;
    const issues = reports.reduce((sum, report) => sum + ((report.issues || []).length), 0);
    el.qaSummary.textContent = `ตรวจแล้ว ${reports.length} ตอน • คะแนนเฉลี่ย ${avg}/100 • พบประเด็น ${issues} จุด`;
    el.qaScoreStrip.innerHTML = `
      <span class="badge badge-success">90–100: ${good} ตอน</span>
      <span class="badge qa-summary-review">75–89: ${review} ตอน</span>
      <span class="badge qa-summary-bad">ต่ำกว่า 75: ${poor} ตอน</span>`;
    const repairable = reports
      .filter(report => Number(report.score) < 90)
      .sort((a, b) => Number(a.score) - Number(b.score))
      .slice(0, 8);
    el.qaRepairList.innerHTML = repairable.length
      ? `<div class="qa-repair-heading">ตอนที่ควรตรวจ/ซ่อมก่อน</div>${repairable.map(report => `
        <button type="button" class="qa-repair-item" data-qa-repair="${report.chapterNo}">
          <span>ตอน ${report.chapterNo}</span><strong>QA ${report.score}</strong><small>${(report.issues || []).length} จุด</small>
        </button>`).join('')}`
      : '<div class="qa-repair-clean">✓ ไม่มีตอนที่ QA ต่ำกว่า 90</div>';
  }

  function appendMemoryCharacterRow(character = {}) {
    const row = document.createElement('div');
    row.className = 'memory-character-row';
    row.innerHTML = `
      <div class="memory-character-grid">
        <input class="form-input" data-field="sourceName" value="${escapeHTML(character.sourceName || '')}" placeholder="ชื่อเดิม เช่น 曹星">
        <input class="form-input" data-field="thaiName" value="${escapeHTML(character.thaiName || '')}" placeholder="ชื่อไทย เช่น เฉาซิง">
        <input class="form-input" data-field="role" value="${escapeHTML(character.role || '')}" placeholder="บทบาท เช่น ตัวเอก">
        <input class="form-input" data-field="gender" value="${escapeHTML(character.gender || '')}" placeholder="เพศ / อัตลักษณ์">
        <input class="form-input" data-field="pronouns" value="${escapeHTML(character.pronouns || '')}" placeholder="สรรพนาม เช่น เขา">
        <input class="form-input" data-field="notes" value="${escapeHTML(character.notes || '')}" placeholder="โน้ตความสัมพันธ์/บุคลิก">
      </div>
      <div class="memory-character-actions">
        <button type="button" class="btn btn-outline btn-sm btn-remove-memory-character">ลบ</button>
      </div>`;
    el.memoryCharacters.appendChild(row);
  }

  function renderMemoryCharacters() {
    el.memoryCharacters.innerHTML = '';
    const characters = state.novelMemory.characters || [];
    characters.forEach(character => appendMemoryCharacterRow(character));
    if (characters.length === 0) appendMemoryCharacterRow();
  }
  function collectMemoryCharacters() {
    return Array.from(el.memoryCharacters.querySelectorAll('.memory-character-row')).map(row => {
      const value = field => row.querySelector(`[data-field="${field}"]`)?.value.trim() || '';
      return {
        sourceName: value('sourceName'),
        thaiName: value('thaiName'),
        role: value('role'),
        gender: value('gender'),
        pronouns: value('pronouns'),
        notes: value('notes'),
      };
    }).filter(character => character.sourceName || character.thaiName);
  }

  async function saveMemory(event) {
    event.preventDefault();
    if (!state.currentSlug || !loaded) return;
    const id = session, slug = state.currentSlug;
    const memory = {
      novelSlug: state.currentSlug,
      storySummary: el.memorySummary.value.trim(),
      facts: el.memoryFacts.value.split('\n').map(value => value.trim()).filter(Boolean),
      characters: collectMemoryCharacters(),
    };
    try {
      const saved = await api(`/api/novels/${state.currentSlug}/memory`, {
        method: 'POST',
        body: JSON.stringify(memory),
      });
      if (!isCurrent(id, slug)) return;
      state.novelMemory = saved;
      el.memoryAiStatus.textContent = 'บันทึก Memory แล้ว — การแปลครั้งถัดไปจะใช้บริบทชุดนี้';
      showToast('บันทึก Story Memory เรียบร้อยแล้ว', 'success');
    } catch (err) {
      console.error('save memory failed', err);
    }
  }

  async function generateMemoryDraft() {
    if (!state.currentSlug || !loaded || el.btnGenerateMemory.disabled) return;
    const id = session, slug = state.currentSlug, edits = editVersion;
    const startChapter = Number(el.memoryAiStart.value || 0);
    const endChapter = Number(el.memoryAiEnd.value || 0);
    const originalText = el.btnGenerateMemory.textContent;
    el.btnGenerateMemory.disabled = true;
    el.btnGenerateMemory.textContent = '⏳ กำลังสกัด Memory...';
    el.memoryAiStatus.textContent = 'กำลังอ่านบริบทและสร้าง Draft — ยังไม่มีการบันทึก';
    try {
      const res = await api(`/api/novels/${state.currentSlug}/memory/generate`, {
        method: 'POST',
        body: JSON.stringify({ startChapter, endChapter }),
      });
      if (!isCurrent(id, slug)) return;
      if (edits !== editVersion) {
        el.memoryAiStatus.textContent = 'มีการแก้ไขระหว่างสร้าง Draft จึงเก็บการแก้ไขของคุณไว้';
        return;
      }
      applyMemoryToForm(res.merged || res.candidate || {});
      el.memoryAiStatus.textContent = `Draft จากตอน ${res.startChapter}–${res.endChapter} (${res.chaptersUsed} ตอน) · ${res.provider}/${res.model} · ยังไม่บันทึก`;
      showToast('สร้าง Memory Draft แล้ว — ตรวจแก้ก่อนกดบันทึก', 'success');
    } catch (err) {
      if (!isCurrent(id, slug)) return;
      el.memoryAiStatus.textContent = `สร้าง Draft ไม่สำเร็จ: ${err.message || err}`;
      console.error('generate memory draft failed', err);
    } finally {
      el.btnGenerateMemory.disabled = false;
      el.btnGenerateMemory.textContent = originalText;
    }
  }

  async function rebuildQA() {
    if (!state.currentSlug) return;
    const id = session, slug = state.currentSlug;
    const originalText = el.btnRebuildQA.textContent;
    el.btnRebuildQA.disabled = true;
    el.btnRebuildQA.textContent = '⏳ กำลังสแกน...';
    try {
      const res = await api(`/api/novels/${state.currentSlug}/qa/rebuild`, { method: 'POST' });
      if (!isCurrent(id, slug)) return;
      state.qaReports = res.reports || [];
      renderQASummary();
      await loadChapters(state.currentSlug);
      showToast(`ตรวจ QA ย้อนหลังแล้ว ${res.rebuilt || 0} ตอน`, 'success');
    } catch (err) {
      console.error('rebuild QA failed', err);
    } finally {
      el.btnRebuildQA.disabled = false;
      el.btnRebuildQA.textContent = originalText;
    }
  }

  async function repairQAChapter(chapterNo, button) {
    if (!state.currentSlug || !chapterNo) return;
    const id = session, slug = state.currentSlug;
    const originalText = button?.textContent || '';
    if (button) {
      button.disabled = true;
      button.textContent = `กำลังซ่อมตอน ${chapterNo}...`;
    }
    try {
      const res = await api(`/api/novels/${state.currentSlug}/qa/${chapterNo}/repair`, {
        method: 'POST',
        body: JSON.stringify({ targetScore: 90 }),
      });
      if (!isCurrent(id, slug)) return;
      const report = res.report;
      if (report) {
        state.qaReports = (state.qaReports || []).filter(item => Number(item.chapterNo) !== Number(chapterNo));
        state.qaReports.push(report);
      }
      renderQASummary();
      await loadChapters(state.currentSlug);
      if (res.improved) {
        showToast(`ตอน ${chapterNo}: QA ดีขึ้นเป็น ${report?.score ?? res.candidateScore}`, 'success');
      } else {
        showToast(`ตอน ${chapterNo}: candidate ไม่ดีขึ้น จึงเก็บคำแปลเดิม`, 'warning');
      }
    } catch (err) {
      console.error('QA repair failed', err);
    } finally {
      if (button && button.isConnected) {
        button.disabled = false;
        button.textContent = originalText;
      }
    }
  }

  function bindIntelligenceEvents() {
    el.formMemory?.addEventListener('input', () => { editVersion++; });
    el.btnOpenIntelligence?.addEventListener('click', openIntelligenceModal);
    el.btnCloseIntelligence?.addEventListener('click', () => closeModal(el.modalIntelligence));
    el.btnAddMemoryCharacter?.addEventListener('click', () => { if (loaded) { editVersion++; appendMemoryCharacterRow(); } });
    el.formMemory?.addEventListener('submit', saveMemory);
    el.btnGenerateMemory?.addEventListener('click', generateMemoryDraft);
    el.btnRebuildQA?.addEventListener('click', rebuildQA);
    el.qaRepairList?.addEventListener('click', event => {
      const button = event.target.closest('[data-qa-repair]');
      if (!button) return;
      repairQAChapter(Number(button.dataset.qaRepair), button);
    });
    el.memoryCharacters?.addEventListener('click', event => {
      const button = event.target.closest('.btn-remove-memory-character');
      if (!button) return;
      editVersion++;
      button.closest('.memory-character-row')?.remove();
    });
  }

  return { openIntelligenceModal, renderQASummary, bindIntelligenceEvents };
}
