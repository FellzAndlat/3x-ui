import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {createRequire} from 'node:module';
import test from 'node:test';
import vm from 'node:vm';
const require = createRequire(new URL('../../../frontend/package.json', import.meta.url));
const {JSDOM} = require('jsdom');
const load = (name) => readFileSync(new URL(`assets/${name}`, import.meta.url), 'utf8');
const settings = (window, enabled) => window.dispatchEvent(new window.MessageEvent('message', {source:window,origin:window.location.origin,data:{type:'3x-ui-youtube-settings',enabled}}));
function mainHarness(response) {
 const dom = new JSDOM('',{url:'https://www.youtube.com/watch?v=test',runScripts:'outside-only'});
 dom.window.Response=Response;dom.window.Headers=Headers;dom.window.fetch=async () => response;
 vm.runInContext(load('main.js'),dom.getInternalVMContext());return dom;
}
test('player JSON removes only ad fields and retains media/auth/captions, URL and clone semantics',async () => {
 const data={adPlacements:[1],playerAds:[2],adSlots:[3],streamingData:{adaptiveFormats:[{url:'https://media.example/video'}]},captions:{tracks:[1]},playabilityStatus:{status:'OK'}};
 const response=new Response(JSON.stringify(data),{headers:{'content-type':'application/json','content-length':'999'}});Object.defineProperty(response,'url',{value:'https://www.youtube.com/youtubei/v1/player'});
 const dom=mainHarness(response);settings(dom.window,true);
 const filtered=await dom.window.fetch('/youtubei/v1/player');assert.notEqual(filtered,response);
 const clone=filtered.clone();assert.equal(filtered.url,response.url);assert.equal(clone.url,response.url);assert.equal(filtered.headers.get('content-length'),null);
 const result=await filtered.json();assert.equal(result.adSlots,undefined);assert.deepEqual(result.streamingData,data.streamingData);assert.deepEqual(result.captions,data.captions);assert.deepEqual(await clone.json(),result);dom.window.close();
});
test('disabled mode, cross-origin responses, ordinary APIs and ad-free player data are untouched',async () => {
 const response=new Response('{ "streamingData": {}, "adSlots": [1] }',{headers:{'content-type':'application/json'}});
 const dom=mainHarness(response);
 assert.equal(await dom.window.fetch('/youtubei/v1/player'),response);settings(dom.window,true);
 assert.equal(await dom.window.fetch('https://other.example/youtubei/v1/player'),response);
 assert.equal(await dom.window.fetch('/youtubei/v1/browse'),response);
 settings(dom.window,false);assert.equal(await dom.window.fetch('/youtubei/v1/player'),response);dom.window.close();
 const adFree=new Response('{ "streamingData": {} }',{headers:{'content-type':'application/json'}});const clean=mainHarness(adFree);settings(clean.window,true);assert.equal(await clean.window.fetch('/youtubei/v1/player'),adFree);clean.window.close();
});
test('initial player object is cleaned without modifying the caller object or read-only properties',() => {
 const dom=mainHarness(new Response(''));settings(dom.window,true);const initial={adSlots:[1],streamingData:{url:'preserved'}};
 dom.window.ytInitialPlayerResponse=initial;assert.equal(dom.window.ytInitialPlayerResponse.adSlots,undefined);assert.deepEqual(initial.adSlots,[1]);assert.equal(dom.window.ytInitialPlayerResponse.streamingData.url,'preserved');dom.window.close();
});
function contentHarness(adShowing, duration=20) {
 const dom=new JSDOM(`<div id="movie_player" class="${adShowing ? 'ad-showing' : ''}"><video></video></div>`,{url:'https://www.youtube.com/watch?v=test',runScripts:'outside-only'});
 const video=dom.window.document.querySelector('video');Object.defineProperty(video,'duration',{value:duration});Object.defineProperty(video,'paused',{value:false});Object.defineProperty(video,'seekable',{value:{length:1}});
 let change;dom.window.chrome={runtime:{},storage:{local:{get:(_defaults,cb)=>cb({enabled:true})},onChanged:{addListener:(cb)=>{change=cb;}}}};
 dom.window.requestAnimationFrame=(cb)=>{cb();return 1;};dom.window.postMessage=()=>{};
 vm.runInContext(load('content.js'),dom.getInternalVMContext());return {dom,video,disable:()=>change({enabled:{newValue:false}},'local')};
}
test('fallback seeks only marked finite ads and disabling restores cosmetic filtering',() => {
 const ordinary=contentHarness(false);assert.equal(ordinary.video.currentTime,0);ordinary.disable();ordinary.dom.window.close();
 const live=contentHarness(true,Infinity);assert.equal(live.video.currentTime,0);live.disable();live.dom.window.close();
 const ad=contentHarness(true);assert.equal(ad.video.currentTime,19.95);assert.equal(ad.dom.window.document.documentElement.hasAttribute('data-3x-ui-youtube'),true);ad.disable();assert.equal(ad.dom.window.document.documentElement.hasAttribute('data-3x-ui-youtube'),false);ad.dom.window.close();
});
test('manifest grants only storage and restricts execution to HTTPS YouTube pages',() => {
 const manifest=JSON.parse(load('manifest.json'));assert.equal(manifest.manifest_version,3);assert.deepEqual(manifest.permissions,['storage']);
 for (const entry of manifest.content_scripts) assert.deepEqual(entry.matches,['https://www.youtube.com/*','https://m.youtube.com/*']);
 assert.equal(manifest.content_scripts[0].world,'MAIN');
});

test('read-only initial player properties remain untouched', () => {
 const dom=new JSDOM('',{url:'https://www.youtube.com/',runScripts:'outside-only'});
 const original={adSlots:[1]};Object.defineProperty(dom.window,'ytInitialPlayerResponse',{value:original,writable:false,configurable:false});
 vm.runInContext(load('main.js'),dom.getInternalVMContext());settings(dom.window,true);assert.equal(dom.window.ytInitialPlayerResponse,original);dom.window.close();
});
