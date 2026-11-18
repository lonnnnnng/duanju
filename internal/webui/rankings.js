(() => {
  'use strict';
  window.DuanjuRankings = {init};

  function init({api, post, getDrama = () => null, onDownloadsChanged, getDownloadQuality = () => 0, canDownload = () => true, play}) {
    const $ = id => document.getElementById(id);
    const categoryRow = $('rankingCategories');
    const boardRow = $('rankingBoards');
    const filterWrap = $('rankingFilterWrap');
    const filterSelect = $('rankingFilter');
    const list = $('rankingList');
    const stateBox = $('rankingState');
    const content = $('rankingContent');
    const moreBtn = $('rankingMoreBtn');
    const refreshBtn = $('refreshRankingsBtn');
    let catalog = null, category = null, board = null, filter = '';
    let items = [], cursor = null, busy = false, controller = null, sequence = 0, loaded = false;
    const submitted = new Set(), submitting = new Set();

    function node(tag, className, text) {
      const result = document.createElement(tag);
      if (className) result.className = className;
      if (text !== undefined) result.textContent = text;
      return result;
    }

    function action(text, handler) {
      const result = node('button', 'secondary', text);
      result.type = 'button';
      result.addEventListener('click', handler);
      return result;
    }

    function setError(text) { $('rankingError').textContent = text || ''; }
    function setState(text) { stateBox.textContent = text || ''; stateBox.hidden = !text; }
    function setBusy(value) {
      busy = value;
      content.setAttribute('aria-busy', String(value));
      refreshBtn.disabled = value;
      moreBtn.disabled = value;
      moreBtn.textContent = value ? '加载中…' : '加载更多';
    }

    function cancel() {
      sequence++;
      if (controller) controller.abort();
      controller = null;
    }

    function summary(result) {
      const description = result?.description || board?.description || '按站点名次展示';
      $('rankingSummary').textContent = description + (items.length ? ' · 已加载 ' + items.length + ' 部' : '');
      const fetchedAt = result?.fetchedAt || '';
      $('rankingFetchedAt').textContent = fetchedAt ? (result.stale ? '上次获取：' : '获取于 ') + new Date(fetchedAt).toLocaleString() : '';
    }

    function renderCategories() {
      const categories = catalog?.categories || [];
      categoryRow.replaceChildren(...categories.map(item => {
        const tab = action(item.name, () => selectCategory(item.id));
        tab.id = 'ranking-cat-' + item.id;
        tab.dataset.category = item.id;
        tab.setAttribute('role', 'tab');
        tab.setAttribute('aria-selected', String(category?.id === item.id));
        tab.tabIndex = category?.id === item.id ? 0 : -1;
        return tab;
      }));
    }

    function renderBoards() {
      const boards = category?.boards || [];
      boardRow.replaceChildren(...boards.map(item => {
        const tab = action(item.name, () => selectBoard(item.id));
        tab.id = 'ranking-board-' + item.id;
        tab.dataset.board = item.id;
        tab.setAttribute('role', 'tab');
        tab.setAttribute('aria-selected', String(board?.id === item.id));
        tab.tabIndex = board?.id === item.id ? 0 : -1;
        return tab;
      }));
    }

    function renderFilters() {
      const groups = board?.filters || [];
      filterWrap.hidden = groups.length === 0;
      const options = [new Option('总榜', '')];
      for (const group of groups) {
        const optgroup = document.createElement('optgroup');
        optgroup.label = group.name;
        for (const option of group.options || []) {
          optgroup.appendChild(new Option(option.name, option.id));
        }
        options.push(optgroup);
      }
      filterSelect.replaceChildren(...options);
      const available = [...filterSelect.options].some(option => option.value === filter);
      if (!available) filter = '';
      filterSelect.value = filter;
    }

    function selectCategory(id) {
      if (!catalog) return;
      const next = catalog.categories.find(item => item.id === id);
      if (!next || next.id === category?.id) return;
      category = next;
      board = category.boards[0] || null;
      filter = '';
      renderCategories();
      renderBoards();
      renderFilters();
      loadPage(null);
    }

    function selectBoard(id) {
      const next = category?.boards.find(item => item.id === id);
      if (!next || next.id === board?.id) return;
      board = next;
      filter = '';
      renderBoards();
      renderFilters();
      loadPage(null);
    }

    function selectFilter(id) {
      if (id === filter) return;
      filter = id;
      loadPage(null);
    }

    async function loadCatalog(refresh = false) {
      cancel();
      const ticket = sequence;
      controller = new AbortController();
      setBusy(true);
      setError('');
      setState('正在读取可用榜单…');
      try {
        const result = await api('/api/ui/rankings/catalog' + (refresh ? '?refresh=1' : ''), {signal: controller.signal});
        if (ticket !== sequence) return;
        const categories = Array.isArray(result?.categories) ? result.categories : [];
        if (!categories.length) throw new Error('暂未提供可用榜单');
        catalog = result;
        category = categories.find(item => item.id === result.default?.category) || categories[0];
        board = category.boards.find(item => item.id === result.default?.board) || category.boards[0] || null;
        filter = '';
        loaded = true;
        renderCategories();
        renderBoards();
        renderFilters();
        await loadPage(null);
      } catch (error) {
        if (ticket !== sequence || error.name === 'AbortError') return;
        loaded = false;
        setState('点击“刷新”重试');
        setError(error.message);
        setBusy(false);
      }
    }

    async function loadPage(next, refresh = false) {
      if (!board) return;
      cancel();
      const ticket = sequence;
      controller = new AbortController();
      setBusy(true);
      setError('');
      const params = new URLSearchParams({category: category.id, board: board.id, filter, offset: String(next?.offset || 0)});
      if (next?.session) params.set('session', next.session);
      if (next?.rankVersion) params.set('rankVersion', next.rankVersion);
      if (next?.filterIds) params.set('filterIds', next.filterIds);
      if (refresh) params.set('refresh', '1');
      if (!next) {
        items = [];
        list.replaceChildren();
        content.scrollTop = 0;
        setState('正在获取站点榜单…');
        moreBtn.hidden = true;
      }
      try {
        const result = await api('/api/ui/rankings/app?' + params.toString(), {signal: controller.signal});
        if (ticket !== sequence) return;
        if (result.category !== category.id || result.board !== board.id || !Array.isArray(result.items)) throw new Error('榜单响应不完整，请刷新重试');
        const seen = new Set(items.map(item => item.id));
        let added = 0;
        for (const item of result.items) {
          if (!item.id || seen.has(item.id)) continue;
          seen.add(item.id);
          items.push(item);
          list.appendChild(renderEntry(item));
          added++;
        }
        cursor = result.hasMore && result.nextOffset > (next?.offset || 0) ? {
          offset: result.nextOffset,
          session: result.nextSession || '',
          rankVersion: result.nextVersion || '',
          filterIds: result.nextFilterIds || ''
        } : null;
        moreBtn.hidden = !cursor;
        setState(items.length ? '' : '此榜单暂时没有条目');
        summary(result);
        setError(result.warning || '');
        if (added === 0 && !items.length) setState('此榜单暂时没有条目');
      } catch (error) {
        if (ticket !== sequence || error.name === 'AbortError') return;
        setError(error.message + (items.length ? '；已保留当前列表。' : ''));
        if (!items.length) setState('暂时无法取得榜单，点击“刷新”重试');
      } finally {
        if (ticket === sequence) {
          controller = null;
          setBusy(false);
        }
      }
    }

    function dramaFor(item) {
      const known = item.drama ? getDrama(item.drama.id) || {} : {};
      if ((Date.parse(known.sortMetadata?.checkedAt) || 0) > (Date.parse(item.fetchedAt) || 0)) return {...item.drama, ...known};
      const drama = {...known, ...(item.drama || {})};
      for (const [key, value] of Object.entries(known)) {
        if (drama[key] == null || drama[key] === '') drama[key] = value;
      }
      return drama;
    }

    function renderEntry(item) {
      const drama = item.drama ? dramaFor(item) : null;
      const title = item.title || drama?.title || '短剧';
      const row = node('li', 'rankings-row');
      row.dataset.kind = item.kind || 'drama';
      const rank = node('span', 'rankings-number' + (item.rank <= 3 ? ' top' : ''), String(item.rank).padStart(2, '0'));
      rank.setAttribute('aria-label', '第 ' + item.rank + ' 名');
      const cover = node('div', 'rankings-cover');
      if (item.cover) {
        const image = document.createElement('img');
        image.loading = 'lazy';
        image.decoding = 'async';
        image.alt = '';
        image.src = item.cover;
        image.addEventListener('error', () => { cover.classList.add('empty'); image.remove(); });
        cover.appendChild(image);
      } else {
        cover.classList.add('empty');
      }
      const body = node('div', 'rankings-body');
      const heading = node('div', 'rankings-title-row');
      const name = node('div', 'rankings-title', title);
      name.title = title;
      heading.appendChild(name);
      if (item.label) heading.appendChild(node('span', 'rankings-label', item.label));
      body.appendChild(heading);
      const meta = [item.metric, item.subtitle].filter(Boolean).join(' · ');
      if (meta) body.appendChild(node('div', 'rankings-meta', meta));
      if (item.description) body.appendChild(node('p', 'rankings-desc', item.description));
      if (Array.isArray(item.tags) && item.tags.length) {
        const tags = node('div', 'rankings-tags');
        for (const tag of item.tags.slice(0, 4)) tags.appendChild(node('span', 'rankings-tag', tag));
        body.appendChild(tags);
      }
      const actions = node('div', 'rankings-actions');
      if (item.playable && drama) {
        const watchLabel = window.DuanjuHistory?.get(drama.id) ? '继续观看' : '播放';
        const watch = action(watchLabel, () => play(drama.id, title));
        watch.setAttribute('aria-label', watchLabel + ' ' + title);
        actions.appendChild(watch);
        if (canDownload()) {
          const download = action(submitted.has(item.id) ? '已提交' : '下载', () => submitDownload(item, drama, download));
          download.disabled = submitted.has(item.id) || submitting.has(item.id);
          download.setAttribute('aria-label', '下载 ' + title);
          actions.appendChild(download);
        }
      } else {
        actions.appendChild(node('span', 'rankings-label muted', item.kind === 'person' ? '人物' : '暂不可播放'));
      }
      row.append(rank, cover, body, actions);
      return row;
    }

    async function submitDownload(item, drama, button) {
      if (!drama?.id || submitted.has(item.id) || submitting.has(item.id)) return;
      submitting.add(item.id);
      button.disabled = true;
      button.textContent = '提交中';
      try {
        await post('/api/ui/download', {ids: [drama.id], quality: getDownloadQuality()});
        submitted.add(item.id);
        button.textContent = '已提交';
        $('rankingActionStatus').textContent = '已将《' + (item.title || drama.title || '短剧') + '》加入下载队列';
        onDownloadsChanged?.();
      } catch (error) {
        button.disabled = false;
        button.textContent = '下载';
        $('rankingActionStatus').textContent = '加入下载失败：' + error.message;
      } finally {
        submitting.delete(item.id);
      }
    }

    refreshBtn.addEventListener('click', () => {
      if (!catalog) loadCatalog(true);
      else loadPage(null, true);
    });
    moreBtn.addEventListener('click', () => { if (busy || !cursor) return; loadPage(cursor); });
    filterSelect.addEventListener('change', () => selectFilter(filterSelect.value));
    [categoryRow, boardRow].forEach(row => row.addEventListener('keydown', event => {
      const buttons = [...row.children], index = buttons.indexOf(event.target);
      if (index < 0 || !['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return;
      event.preventDefault();
      const next = event.key === 'Home' ? 0 : event.key === 'End' ? buttons.length - 1 : (index + (event.key === 'ArrowRight' ? 1 : -1) + buttons.length) % buttons.length;
      buttons[next].focus();
      buttons[next].click();
    }));

    return {
      load: () => { if (!loaded) loadCatalog(); },
      refresh: () => { if (!catalog) loadCatalog(true); else loadPage(null, true); }
    };
  }
})();
