import { $, element, button, readPreference, savePreference, populateIcons } from './ui-core.js';

export function createShell(app) {
  const main = $('workspaceMain');
  const positions = {library: 0, following: 0, downloads: 0, settings: 0};
  let current = '', sourceStates = {}, sourceLoading = false, sourceRequest = '';
  let sourceNotice;
  const sourceRows = new Map(), sourceErrors = new Map();

  function setPage(page) {
    if (page === current) return;
    if (current) positions[current] = main.scrollTop;
    current = page;
    document.body.dataset.page = page;
    document.querySelectorAll('[data-view]').forEach(view => view.hidden = view.dataset.view !== page);
    document.querySelectorAll('button[data-page]').forEach(control => {
      if (control.dataset.page === page) control.setAttribute('aria-current', 'page');
      else control.removeAttribute('aria-current');
    });
    if (page === 'following') app.following?.render();
    if (page === 'downloads') app.downloads?.render();
    if (page === 'library') app.library?.layout();
    if (page === 'settings') {
      showSources();
      app.emby?.refresh?.();
      window.DuanjuHistory?.refresh?.();
    }
    requestAnimationFrame(() => {main.scrollTop = positions[page] || 0;});
  }

  function activity(visible) {
    $('activityPanel').hidden = !visible;
    $('catalogLayout').classList.toggle('with-activity', visible);
    $('activityToggle').setAttribute('aria-pressed', String(visible));
    $('activityToggleLabel').textContent = visible ? '收起任务' : '任务侧栏';
    savePreference('activitySidebar', visible);
    if (visible) app.downloads?.render();
  }

  function updateSourceRows() {
    const statuses = {loading: '正在获取', ready: '已就绪', success: '已更新', done: '已更新', failed: '暂不可用', error: '暂不可用', partial: '部分更新', cached: '使用缓存', pending: '等待更新'};
    for (const [id, controls] of sourceRows) {
      const state = sourceStates[id];
      const busy = sourceRequest === id || sourceLoading && state?.status === 'loading';
      controls.status.textContent = sourceRequest === id ? '正在提交' : state ? statuses[state.status] || (state.error ? '暂不可用' : '已有缓存') : '未检查';
      const date = state?.updatedAt && !state.updatedAt.startsWith('0001') ? new Date(state.updatedAt).toLocaleString() : '';
      controls.detail.textContent = [Number.isFinite(state?.count) ? state.count + ' 部' : '', date].filter(Boolean).join(' · ');
      controls.error.textContent = sourceErrors.get(id) || state?.error || '';
      controls.action.disabled = sourceLoading || Boolean(sourceRequest);
      controls.action.textContent = busy ? '更新中…' : '更新';
      controls.action.setAttribute('aria-busy', String(busy));
      controls.action.title = sourceLoading || sourceRequest ? '正在更新剧库，完成后可再次更新' : '只更新' + controls.label + '的目录和资料';
    }
    if (sourceNotice) sourceNotice.textContent = sourceLoading || sourceRequest ? '正在更新剧库，已有内容可继续使用；完成后可单独更新站源。' : '点击对应站源的“更新”可查新、续载和补齐资料，失败时保留已有内容。';
  }

  async function updateSource(id) {
    if (sourceLoading || sourceRequest) return;
    sourceRequest = id;
    sourceErrors.delete(id);
    updateSourceRows();
    try {
      const result = await app.api('/api/ui/dramas?update=1&source=' + encodeURIComponent(id));
      sourceStates = result.sources || {};
      sourceLoading = Boolean(result.loading);
      if (result.updateAccepted === false) sourceErrors.set(id, '本次未提交：已有更新任务，可在完成后重试。');
    } catch (error) {
      sourceErrors.set(id, '更新请求失败：' + error.message);
    } finally {
      sourceRequest = '';
      updateSourceRows();
      void app.library.refresh();
    }
  }

  function showSources() {
    const names = {hongguo: '主站'};
    const content = $('sourceStatusContent');
    sourceRows.clear();
    sourceNotice = null;
    content.replaceChildren();
    for (const [id, label] of Object.entries(names)) {
      const row = element('section', 'source-status-row');
      row.dataset.source = id;
      const heading = element('div', 'heading source-status-heading');
      const actions = element('div', 'source-status-actions');
      const status = element('span', 'tag');
      status.setAttribute('aria-live', 'polite');
      const action = button('更新', () => updateSource(id), false, 'secondary source-update');
      action.setAttribute('aria-label', '更新' + label);
      actions.append(status, action);
      heading.append(element('h3', '', label), actions);
      const detail = element('p', 'small'), error = element('p', 'error');
      error.setAttribute('role', 'status');
      row.append(heading, detail, error);
      sourceRows.set(id, {row, label, status, action, detail, error});
      content.appendChild(row);
    }
    sourceNotice = element('p', 'small notice');
    content.appendChild(sourceNotice);
    updateSourceRows();
    void app.library.refresh();
  }

  function positionMenu(menu) {
    const popover = menu.querySelector('.action-popover');
    if (!menu.open || !popover) return;
    popover.style.maxHeight = '';
    const anchor = menu.getBoundingClientRect();
    const viewport = main.getBoundingClientRect();
    const width = popover.getBoundingClientRect().width;
    const left = Math.max(12, Math.min(anchor.left, window.innerWidth - width - 12));
    const below = Math.max(0, Math.min(window.innerHeight, viewport.bottom) - anchor.bottom - 12);
    const above = Math.max(0, anchor.top - Math.max(0, viewport.top) - 12);
    const height = popover.scrollHeight + 2;
    const upward = height > below && above > below;
    popover.style.left = left - anchor.left + 'px';
    popover.style.right = 'auto';
    popover.style.maxHeight = Math.max(80, upward ? above : below) + 'px';
    popover.style.overflowY = 'auto';
    popover.style.top = upward ? -(Math.min(height, above) + 6) + 'px' : anchor.height + 6 + 'px';
  }

  function access() {
    updateSourceRows();
    activity(readPreference('activitySidebar', false) === true);
    setPage(window.DuanjuDialogs.page());
  }

  function init() {
    populateIcons();
    window.DuanjuDialogs.listen(setPage);
    document.querySelectorAll('button[data-page], [data-go]').forEach(control => control.addEventListener('click', () => {
      if (control.dataset.go === 'following') app.following.showTab('watching');
      window.DuanjuDialogs.navigate(control.dataset.page || control.dataset.go);
    }));
    document.querySelector('.brand').addEventListener('click', event => {event.preventDefault(); window.DuanjuDialogs.navigate('library');});
    document.querySelector('.skip-link').addEventListener('click', event => {event.preventDefault(); main.focus();});
    $('activityToggle').addEventListener('click', () => activity($('activityPanel').hidden));
    $('closeActivityBtn').addEventListener('click', () => activity(false));
    document.addEventListener('click', event => {
      for (const menu of document.querySelectorAll('.action-menu[open]')) {
        if (!menu.contains(event.target) || event.target.closest('button')) menu.open = false;
      }
    });
    document.addEventListener('toggle', event => {
      if (event.target.matches?.('.action-menu')) positionMenu(event.target);
    }, true);
    document.addEventListener('keydown', event => {
      if (event.key !== 'Escape' || document.querySelector('dialog[open]')) return;
      const menu = document.querySelector('.action-menu[open]');
      if (!menu) return;
      event.preventDefault();
      menu.open = false;
      menu.querySelector('summary').focus({preventScroll: true});
    });
    let positionMenusFrame = 0;
    const positionMenus = () => {
      if (positionMenusFrame) return;
      positionMenusFrame = requestAnimationFrame(() => {
        positionMenusFrame = 0;
        document.querySelectorAll('.action-menu[open]').forEach(positionMenu);
      });
    };
    window.addEventListener('resize', positionMenus);
    main.addEventListener('scroll', positionMenus, {passive: true});
    activity(readPreference('activitySidebar', false) === true);
    setPage(window.DuanjuDialogs.page());
  }

  return {init, access, page: () => current, resetScroll: () => {main.scrollTop = 0;}, sourceStates: (value, loading) => {sourceStates = value || {}; sourceLoading = Boolean(loading); updateSourceRows();}};
}
