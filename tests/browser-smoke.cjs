// Development-only: NODE_PATH may point at an existing Playwright installation.
// Run against a freshly built binary. All books/configuration are synthetic.
const { chromium } = require('playwright');
const { spawn } = require('node:child_process');
const fs = require('node:fs/promises');
const path = require('node:path');
const os = require('node:os');
const assert = require('node:assert/strict');

(async () => {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), 'novelclaw-smoke-'));
  const data = path.join(root, 'novels');
  const chaptersDir = path.join(data, 'smoke', 'chapters');
  await fs.mkdir(chaptersDir, { recursive: true });
  await fs.writeFile(path.join(data, 'smoke', 'novel.json'), JSON.stringify({slug: 'smoke', title: 'นิยายทดสอบการอ่าน', totalChapters: 10000, translatedChapters: 3}));
  for (const [slug,title,genre,author] of [['moon','จดหมายจากปลายจันทร์','fantasy','ลลิน'],['winter','ฤดูหนาวครั้งสุดท้าย','apocalypse','ณ วันที่ฝนพรำ'],['river','เสียงกระซิบของสายน้ำ','romance','พิมพ์ดาว']]) {
    await fs.mkdir(path.join(data,slug),{recursive:true});
    await fs.writeFile(path.join(data,slug,'novel.json'),JSON.stringify({slug,title,genre,author,description:'เรื่องราวที่รอให้เปิดอ่าน ในจังหวะของคุณเอง'}));
  }
  await fs.writeFile(path.join(chaptersDir, 'catalog.json'), JSON.stringify(Array.from({length: 10000}, (_, i) => ({chapterNo: i + 1, titleSource: `ตอนที่ ${i+1}`, hasSource: i < 3, hasTranslated: i < 3, locked: i >= 3}))));
  for (let i = 1; i <= 3; i++) {
    const text = Array.from({length: 200}, (_, n) => `ย่อหน้า ${n+1} ในตอนที่ ${i} ` + 'ลมเย็นพัดผ่านหน้าต่าง เขาเปิดหนังสือและเริ่มอ่านเรื่องราวอย่างเงียบสงบ '.repeat(4));
    await fs.writeFile(path.join(chaptersDir, `${String(i).padStart(4,'0')}.th.json`), JSON.stringify({title: {source: `ตอนที่ ${i}`, translated: `บททดสอบ ${i}`}, paragraphs: text}));
  }
  const port = Number(process.env.NC_SMOKE_PORT || 14891);
  const base = `http://127.0.0.1:${port}`;
  const server = spawn(path.resolve(process.env.NC_BINARY || './novelclaw.exe'), ['-host', '127.0.0.1', '-port', String(port), '-browser=false', '-data', data, '-config', path.join(root, 'config.json')], {cwd: root, windowsHide: true, stdio: 'ignore'});
  let browser;
  try {
    for (let i = 0; i < 100; i++) {
      if (await fetch(base).then(r => r.ok).catch(() => false)) break;
      await new Promise(resolve => setTimeout(resolve, 100));
    }
    browser = await chromium.launch({headless: true});
    const page = await browser.newPage({viewport: {width: 1280, height: 800}});
    const errors = [], requests = [];
    page.on('pageerror', error => errors.push(error.message));
    page.on('request', request => requests.push(request.url()));
    await page.goto(base);
    await page.waitForFunction(() => document.documentElement.dataset.appReady === 'true');
    assert.equal(requests.some(url => /\/models/.test(url)), false, 'opening the library must not discover cloud models');
    await page.locator('#library-search').fill('ไม่มีเรื่องนี้');
    await page.getByRole('button',{name:'ล้างการค้นหา'}).waitFor();
    assert.equal(await page.locator('.novel-card').count(),0);
    await page.getByRole('button',{name:'ล้างการค้นหา'}).click();
    assert.equal(await page.locator('.novel-card').count(),4);
    await page.locator('#library-sort').selectOption('title');
    const capture = async name => { if(process.env.NC_SCREENSHOT) await page.screenshot({path:path.join(path.dirname(process.env.NC_SCREENSHOT),`redesign-${name}.png`),fullPage:false}); };
    await capture('library');
    for(const width of [320,390]) {
      await page.setViewportSize({width,height:844});
      assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'library fits mobile');
    }
    await capture('library-mobile');
    await page.setViewportSize({width:1280,height:800});
    await page.locator('.novel-card[data-slug="smoke"]').click();
    await page.locator('.chapter-item').first().waitFor();
    assert.equal(await page.locator('.chapter-item').count(), 50, '10,000 chapters render one page');
    await capture('chapters');
    await page.locator('[data-continue-ch]').click();
    await page.locator('#reader-content p').first().waitFor();
    await page.waitForTimeout(120);
    assert.match(await page.locator('#reader-chapter-title').innerText(), /บททดสอบ 1/);
    const before = await page.locator('#reader-content').evaluate(node => parseFloat(getComputedStyle(node).fontSize));
    await page.locator('#btn-font-decrease').click();
    const after = await page.locator('#reader-content').evaluate(node => parseFloat(getComputedStyle(node).fontSize));
    assert.equal(after, before - 2, 'font decrease visibly changes the font');
    await page.evaluate(() => scrollTo(0, 900));
    await page.waitForTimeout(100);
    await page.keyboard.press('ArrowRight');
    await page.waitForFunction(() => document.querySelector('#reader-chapter-title').textContent.includes('บททดสอบ 2'));
    assert.ok(await page.evaluate(() => Number(localStorage.getItem('nc_scroll_smoke_1'))) >= 850, 'navigation flushes the outgoing scroll');
    await page.keyboard.press('ArrowLeft');
    await page.waitForFunction(() => document.querySelector('#reader-chapter-title').textContent.includes('บททดสอบ 1') && scrollY >= 850);
    await page.locator('#btn-reader-translate').click();
    await page.keyboard.press('ArrowRight');
    assert.match(await page.locator('#reader-chapter-title').innerText(), /บททดสอบ 1/, 'reader shortcuts do not navigate behind a modal');
    await page.keyboard.press('Escape');
    const cdp = await page.context().newCDPSession(page);
    await cdp.send('Performance.enable');
    const metric = async () => Object.fromEntries((await cdp.send('Performance.getMetrics')).metrics.map(m => [m.name, m.value]));
    const start = await metric(); await page.waitForTimeout(2000); const end = await metric();
    const report = {chapters: 10000, renderedChapterRows: 50, readerParagraphs: 200, idleTaskMsOver2s: (end.TaskDuration-start.TaskDuration)*1000, jsHeapMiB: end.JSHeapUsedSize/1048576, errors};
    for (const width of [390, 320]) {
      await page.setViewportSize({width, height: 844});
      await page.evaluate(() => scrollTo(0, 0));
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, `no page overflow at ${width}px`);
      await page.locator('#btn-reader-mode').click();
      await page.locator('#btn-reader-theme').click();
      assert.equal(await page.locator('.reader-toolbar').evaluate(node => node.scrollWidth <= node.clientWidth), true, `all toolbar controls fit at ${width}px`);
    }
    await page.waitForTimeout(4500); // Let informational toasts clear in the saved preview.
    if (process.env.NC_SCREENSHOT) await page.screenshot({path: process.env.NC_SCREENSHOT, fullPage: false});
    const apiChecks = [];
    assert.equal((await page.request.get(`${base}/api/not-a-route`)).status(), 404);
    apiChecks.push('unknown API returns JSON 404');
    const imported = await page.request.post(`${base}/api/import`, {data: {novelSlug:'manual',novelTitle:'Manual fixture',startChapter:1,rawContent:'First paragraph.\nSecond paragraph.'}});
    assert.equal(imported.status(),200);
    const manual = await (await page.request.get(`${base}/api/novels/manual/chapters/1`)).json();
    assert.equal(manual.sourceText.length,2);
    apiChecks.push('paste import and read');
    for (const format of ['txt','markdown','epub']) {
      const download = await page.request.get(`${base}/api/novels/smoke/export?format=${format}&start=1&end=3`);
      assert.equal(download.status(),200); assert.ok((await download.body()).length>0);
      apiChecks.push(`${format} export`);
    }
    assert.equal((await page.request.post(`${base}/api/backup`)).status(),200);
    apiChecks.push('backup');
    await page.locator('#btn-reader-back').click();
    await page.locator('.detail-tool-menu summary').click();
    await page.locator('#btn-open-glossary').click();
    await page.waitForFunction(()=>!document.querySelector('#btn-save-glossary').disabled);
    await capture('glossary');
    await page.locator('#btn-close-glossary').click();
    await page.locator('#btn-open-intelligence').click();
    await page.waitForFunction(()=>!document.querySelector('#qa-summary').textContent.includes('กำลังโหลด'));
    await page.locator('#btn-close-intelligence').click();
    // Discovery here is explicit UI work, with a deterministic local response.
    await page.route('**/api/models?*',route=>route.fulfill({json:{models:['test-model'],freeModels:[]}}));
    await page.locator('#btn-open-settings').click();
    await page.locator('#cfg-provider').waitFor({state:'visible'});
    assert.ok(await page.locator('#modal-settings').getAttribute('aria-labelledby'));
    await page.setViewportSize({width:1280,height:900});
    await capture('settings');
    for(const theme of ['dark','light','sepia','black']) {
      await page.evaluate(theme=>document.body.dataset.theme=theme,theme);
      await page.setViewportSize({width:320,height:844});
      assert.ok(await page.locator('#modal-settings .modal-content').evaluate(n=>n.scrollWidth<=n.clientWidth),'settings fit '+theme);
    }
    await page.locator('#btn-close-settings').click();
    report.apiChecks=apiChecks;
    report.modals=['glossary','memory/QA','settings'];
    report.redesignChecks=['library search/clear/sort','mobile library 320/390','dialog accessible name','settings mobile in all four themes'];
    assert.deepEqual(errors, []);
    console.log(JSON.stringify(report, null, 2));
  } finally {
    await browser?.close();
    server.kill();
    await new Promise(resolve => server.exitCode !== null ? resolve() : server.once('exit', resolve));
    assert.equal(path.dirname(path.resolve(root)).toLowerCase(),path.resolve(os.tmpdir()).toLowerCase());
    await fs.rm(root, {recursive: true, force: true});
  }
})().catch(error => { console.error(error); process.exitCode = 1; });
