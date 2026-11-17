// Isolated browser for one agent console. stdout is a JSON-lines protocol.
const { chromium } = require(process.argv[1]);
const readline = require('node:readline');
const emit = (value) => process.stdout.write(JSON.stringify(value) + '\n');
let browser, page, capturing = false, queue = Promise.resolve(), mouseX = 0, mouseY = 0;
async function moveTo(x, y) {
  const fromX = mouseX, fromY = mouseY;
  for (let i = 1; i <= 20; i++) {
    await page.mouse.move(fromX + (x-fromX)*i/20, fromY + (y-fromY)*i/20);
    await new Promise(resolve => setTimeout(resolve, 20));
  }
  mouseX = x; mouseY = y;
}
async function installCursor() {
  await page.addInitScript(() => {
    const install = () => {
      const cursor = document.createElement('div');
      cursor.id = 'zamboard-test-cursor';
      cursor.setAttribute('aria-hidden', 'true');
      cursor.style.cssText = 'position:fixed;left:0;top:0;width:18px;height:24px;background:#ffcc33;clip-path:polygon(0 0,0 100%,30% 72%,50% 100%,66% 90%,45% 62%,90% 62%);z-index:2147483647;pointer-events:none;filter:drop-shadow(0 1px 2px black);';
      document.documentElement.append(cursor);
      addEventListener('mousemove', e => { cursor.style.transform = `translate(${e.clientX}px,${e.clientY}px)`; }, true);
      addEventListener('mousedown', e => {
        const ring = document.createElement('div');
        ring.style.cssText = `position:fixed;left:${e.clientX-14}px;top:${e.clientY-14}px;width:28px;height:28px;border:3px solid #ffcc33;border-radius:50%;z-index:2147483646;pointer-events:none;`;
        document.documentElement.append(ring);
        ring.animate([{opacity:1,transform:'scale(.5)'},{opacity:0,transform:'scale(2)'}], {duration:650}).finished.then(() => ring.remove());
      }, true);
    };
    if (document.documentElement) install(); else addEventListener('DOMContentLoaded', install, {once:true});
  });
}
async function capture() {
  if (!page || page.isClosed() || capturing) return;
  capturing = true;
  try { emit({frame:(await page.screenshot({type:'jpeg',quality:65,timeout:3000})).toString('base64')}); }
  catch {} finally { capturing = false; }
}
function locator(a) {
  if (typeof a.selector !== 'string' || !a.selector.trim() || a.selector.length > 2000) throw Error('selector is required (max 2000 characters)');
  return page.locator(a.selector).first();
}
async function snapshot() {
  const elements = await page.locator('a,button,input,textarea,select,[role="button"]').evaluateAll(nodes => nodes.slice(0,150).map((el,i) => {
    el.setAttribute('data-zamboard-id', String(i));
    return {selector:`[data-zamboard-id="${i}"]`,tag:el.tagName,text:(el.innerText || el.getAttribute('aria-label') || el.getAttribute('placeholder') || '').slice(0,160)};
  }));
  return {url:page.url(),title:await page.title(),text:(await page.locator('body').innerText()).slice(0,12000),elements};
}
async function act(a) {
  if (a.action === 'open') {
    const url = new URL(a.url);
    if (!['http:','https:'].includes(url.protocol) || url.username || url.password) throw Error('Use an HTTP(S) URL without credentials');
    if (!browser) {
      browser = await chromium.launch({headless:true});
      const context = await browser.newContext({viewport:{width:1280,height:800},acceptDownloads:false});
      page = await context.newPage();
      page.setDefaultTimeout(10000);
      await installCursor();
    }
    await page.goto(url.href,{waitUntil:'domcontentloaded',timeout:30000});
  } else {
    if (!page || page.isClosed()) throw Error('Open a webpage first');
    switch (a.action) {
      case 'click': case 'hover': case 'fill': {
        const el = locator(a);
        await el.scrollIntoViewIfNeeded();
        const box = await el.boundingBox();
        if (!box) throw Error('Element is not visible');
        await moveTo(box.x+box.width/2,box.y+box.height/2);
        // Leave cursor motion visible before acting.
        await capture();
        if (a.action === 'click') await page.mouse.click(box.x+box.width/2,box.y+box.height/2,{delay:120});
        if (a.action === 'fill') { if (typeof a.text !== 'string' || a.text.length > 20000) throw Error('text must be a string up to 20000 characters'); await el.fill(a.text); }
        break;
      }
      case 'press':
        if (typeof a.key !== 'string' || a.key.length > 100) throw Error('key is required');
        await page.keyboard.press(a.key); break;
      case 'scroll':
        if (!Number.isFinite(a.y) || Math.abs(a.y)>10000) throw Error('y must be a number between -10000 and 10000');
        await page.mouse.wheel(0,a.y); break;
      case 'snapshot': break;
      default: throw Error('Unknown action');
    }
  }
  await capture();
  return snapshot();
}
readline.createInterface({input:process.stdin}).on('line', line => {
  queue = queue.then(async () => {
    try { emit({result:await act(JSON.parse(line))}); }
    catch (e) { emit({error:e.message}); }
  });
});
setInterval(capture,200).unref();
process.stdin.on('end',async () => { if(browser) await browser.close(); process.exit(0); });
