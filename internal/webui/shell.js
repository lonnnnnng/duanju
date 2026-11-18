import { $, readPreference, savePreference, populateIcons } from './ui-core.js';

export function createShell(app) {
  const main = $('workspaceMain');
  const positions = {library: 0, rankings: 0, following: 0, downloads: 0, 'system-settings': 0, 'emby-settings': 0};
  let current = '', sourceStates = {};

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
    if (page === 'rankings') app.rankings?.load();
    if (page === 'following') app.following?.render();
    if (page === 'downloads') app.downloads?.render();
    if (page === 'library') app.library?.layout();
    if (page === 'emby-settings') {
      app.emby?.refresh?.();
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

  function updateHeaderSource() {
    const detail = $('headerSourceDetail');
    if (!detail) return;
    const state = sourceStates.hongguo || Object.values(sourceStates)[0] || {};
    const count = Number.isFinite(state.count) ? state.count + ' 部' : '';
    const date = state.updatedAt && !state.updatedAt.startsWith('0001') ? new Date(state.updatedAt).toLocaleString() : '';
    detail.textContent = [count, date].filter(Boolean).join(' · ') || '站源状态未知';
    detail.title = [state.status ? '状态：' + state.status : '', state.error || ''].filter(Boolean).join(' · ') || '主站剧库状态';
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
    updateHeaderSource();
    activity(readPreference('activitySidebar', false) === true);
    setPage(window.DuanjuDialogs.page());
  }

  function init() {
    populateIcons();
    window.DuanjuDialogs.listen(setPage);
    document.querySelectorAll('button[data-page], [data-go]').forEach(control => control.addEventListener('click', () => {
      if (control.dataset.go === 'following') app.following.showTab('saved');
      window.DuanjuDialogs.navigate(control.dataset.page || control.dataset.go);
    }));
    document.querySelector('.brand').addEventListener('click', event => {event.preventDefault(); window.DuanjuDialogs.navigate('rankings');});
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

  return {init, access, page: () => current, resetScroll: () => {main.scrollTop = 0;}, sourceStates: value => {sourceStates = value || {}; updateHeaderSource();}};
}
