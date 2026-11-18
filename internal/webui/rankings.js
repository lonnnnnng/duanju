(() => {
  'use strict';
  window.DuanjuRankings = {init};

  function init({api, post, icon, getDrama = () => null, openDetails, onDownloadsChanged, getDownloadQuality = () => 0, canDownload = () => true, play}) {
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
        const groupOptions = (group.options || []).filter(option => option.id);
        if (!groupOptions.length) continue;
        const optgroup = document.createElement('optgroup');
        optgroup.label = group.name;
        for (const option of groupOptions) {
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
      const card = node('li', 'card rankings-card');
      card.dataset.kind = item.kind || 'drama';
      const playable = Boolean(item.playable && drama);
      const actionLabel = playable ? '播放' : item.kind === 'person' ? '人物' : item.kind === 'series' ? '系列剧' : '预约';
      const poster = node('button', 'poster watch-button');
      poster.type = 'button';
      poster.setAttribute('aria-label', actionLabel + ' ' + title);
      const cover = item.cover || (drama && (drama.cover || drama.coverUrl)) || '';
      const placeholder = () => {
        const fallback = node('span', 'cover placeholder');
        fallback.setAttribute('aria-hidden', 'true');
        fallback.append(node('span', '', title.slice(0, 1)), node('small', '', '暂无海报'));
        return fallback;
      };
      if (cover) {
        const image = document.createElement('img');
        image.className = 'cover';
        image.loading = 'lazy';
        image.decoding = 'async';
        image.alt = '';
        image.src = cover;
        image.addEventListener('error', () => image.replaceWith(placeholder()));
        poster.appendChild(image);
      } else poster.appendChild(placeholder());
      const rank = node('span', 'rankings-rank' + (item.rank <= 3 ? ' top rank-' + item.rank : ''), String(item.rank).padStart(2, '0'));
      rank.setAttribute('aria-label', '第 ' + item.rank + ' 名');
      poster.appendChild(rank);
      if (item.label) poster.appendChild(node('span', 'rankings-kind', item.label));
      const caption = node('span', 'poster-caption');
      caption.append(icon(item.kind === 'person' ? 'user' : item.kind === 'upcoming' ? 'clock' : 'play'),
        node('span', 'watch-label', actionLabel), node('span', 'poster-category', item.metric || ''));
      poster.appendChild(caption);
      if (playable) poster.addEventListener('click', () => play(drama.id, title));
      else poster.disabled = true;
      const body = node('div', 'card-body');
      const name = drama && openDetails ? node('button', 'card-title', title) : node('div', 'card-title', title);
      name.title = title;
      if (name.tagName === 'BUTTON') {
        name.type = 'button';
        name.addEventListener('click', () => openDetails(drama.id));
      }
      const meta = node('div', 'meta', item.subtitle || item.metric || '');
      meta.title = [item.metric, item.subtitle, item.description].filter(Boolean).join(' · ');
      const footer = node('div', 'card-footer');
      const actions = node('div', 'card-actions');
      if (playable && drama && canDownload()) {
        const download = node('button', 'quiet icon-button card-download');
        download.type = 'button';
        download.appendChild(icon('download'));
        download.title = '加入下载';
        download.setAttribute('aria-label', '下载 ' + title);
        download.addEventListener('click', () => submitDownload(item, drama, download));
        actions.appendChild(download);
      }
      footer.append(meta, actions);
      body.append(name, footer);
      card.append(poster, body);
      return card;
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
