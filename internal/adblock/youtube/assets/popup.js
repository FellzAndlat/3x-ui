'use strict';
const input = document.getElementById('enabled');
const status = document.getElementById('status');
input.disabled = true;
chrome.storage.local.get({enabled: true}, (settings) => {
  if (chrome.runtime.lastError) {status.textContent = 'Не удалось прочитать настройки';return;}
  input.checked = settings.enabled !== false;input.disabled = false;
});
input.addEventListener('change', () => {
  input.disabled = true;
  chrome.storage.local.set({enabled: input.checked}, () => {
    input.disabled = false;
    status.textContent = chrome.runtime.lastError ? 'Не удалось сохранить настройки' : 'Сохранено. Обновите страницу YouTube.';
  });
});
