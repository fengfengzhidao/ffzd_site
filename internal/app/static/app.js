(() => {
  const adminLayout = document.querySelector('[data-admin-layout]');
  if (adminLayout) {
    const toggle = adminLayout.querySelector('[data-sidebar-toggle]');
    const closeButton = adminLayout.querySelector('[data-sidebar-close]');
    const mobileQuery = window.matchMedia('(max-width: 900px)');
    const storageKey = 'admin-sidebar-collapsed';

    const storedCollapsed = () => {
      try { return window.localStorage.getItem(storageKey) === 'true'; }
      catch (_) { return false; }
    };
    const saveCollapsed = (value) => {
      try { window.localStorage.setItem(storageKey, String(value)); }
      catch (_) { /* Storage can be unavailable in private contexts. */ }
    };
    const syncToggle = () => {
      const expanded = mobileQuery.matches
        ? adminLayout.classList.contains('sidebar-open')
        : !adminLayout.classList.contains('sidebar-collapsed');
      toggle?.setAttribute('aria-expanded', String(expanded));
      toggle?.setAttribute('aria-label', mobileQuery.matches
        ? (expanded ? '关闭导航' : '打开导航')
        : (expanded ? '折叠侧栏' : '展开侧栏'));
    };
    const closeDrawer = () => {
      adminLayout.classList.remove('sidebar-open');
      document.body.classList.remove('admin-drawer-open');
      syncToggle();
    };
    const applyMode = () => {
      closeDrawer();
      adminLayout.classList.toggle('sidebar-collapsed', !mobileQuery.matches && storedCollapsed());
      syncToggle();
    };

    applyMode();
    toggle?.addEventListener('click', () => {
      if (mobileQuery.matches) {
        const opening = !adminLayout.classList.contains('sidebar-open');
        adminLayout.classList.toggle('sidebar-open', opening);
        document.body.classList.toggle('admin-drawer-open', opening);
      } else {
        const collapsed = !adminLayout.classList.contains('sidebar-collapsed');
        adminLayout.classList.toggle('sidebar-collapsed', collapsed);
        saveCollapsed(collapsed);
      }
      syncToggle();
    });
    closeButton?.addEventListener('click', closeDrawer);
    document.addEventListener('keydown', (event) => {
      if (event.key === 'Escape' && adminLayout.classList.contains('sidebar-open')) {
        closeDrawer();
        toggle?.focus();
      }
    });
    if (typeof mobileQuery.addEventListener === 'function') mobileQuery.addEventListener('change', applyMode);
  }

  document.querySelectorAll('form[data-confirm]').forEach((form) => {
    form.addEventListener('submit', (event) => {
      if (!window.confirm(form.dataset.confirm)) event.preventDefault();
    });
  });

  const composerLayer = document.querySelector('[data-composer-layer]');
  const infoLayer = composerLayer?.querySelector('[data-info-layer]');
  const openComposer = () => {
    if (!composerLayer) return;
    composerLayer.classList.add('is-open');
    composerLayer.setAttribute('aria-hidden', 'false');
    document.body.classList.add('composer-open');
    window.setTimeout(() => composerLayer.querySelector('#markdown-input')?.focus(), 180);
  };
  const openInfo = () => {
    if (!infoLayer) return;
    infoLayer.classList.add('is-open');
    infoLayer.setAttribute('aria-hidden', 'false');
    window.setTimeout(() => infoLayer.querySelector('input[name="title"]')?.focus(), 80);
  };
  const closeInfo = () => {
    if (!infoLayer) return;
    infoLayer.classList.remove('is-open');
    infoLayer.setAttribute('aria-hidden', 'true');
  };
  const closeComposer = () => {
    if (!composerLayer) return;
    closeInfo();
    composerLayer.classList.remove('is-open');
    composerLayer.setAttribute('aria-hidden', 'true');
    document.body.classList.remove('composer-open');
    if (new URLSearchParams(window.location.search).has('compose')) history.replaceState({}, '', '/admin/posts');
  };
  document.querySelector('[data-composer-open]')?.addEventListener('click', openComposer);
  composerLayer?.querySelectorAll('[data-composer-close]').forEach((button) => button.addEventListener('click', closeComposer));
  composerLayer?.querySelector('[data-info-open]')?.addEventListener('click', openInfo);
  infoLayer?.querySelectorAll('[data-info-close]').forEach((button) => button.addEventListener('click', closeInfo));
  infoLayer?.querySelector('[data-info-save]')?.addEventListener('click', () => {
    const title = infoLayer.querySelector('input[name="title"]');
    const error = infoLayer.querySelector('[data-info-error]');
    if (!title?.value.trim()) { error.hidden = false; title?.focus(); return; }
    error.hidden = true;
    closeInfo();
  });
  if (composerLayer?.classList.contains('is-open')) document.body.classList.add('composer-open');
  document.addEventListener('keydown', (event) => {
    if (event.key !== 'Escape') return;
    if (infoLayer?.classList.contains('is-open')) closeInfo();
    else if (composerLayer?.classList.contains('is-open')) closeComposer();
  });

  document.querySelectorAll('[data-tag-combobox]').forEach((box) => {
    const input = box.querySelector('[data-tag-input]');
    const hidden = box.querySelector('[data-tag-hidden]');
    const values = box.querySelector('[data-tag-values]');
    let tags = (box.dataset.initialTags || '').split(/[,，]/).map((value) => value.trim()).filter(Boolean);
    const sync = () => {
      hidden.value = tags.join(', ');
      values.replaceChildren(...tags.map((tag) => {
        const chip = document.createElement('button');
        chip.type = 'button';
        chip.className = 'tag-value';
        chip.textContent = `${tag} ×`;
        chip.setAttribute('aria-label', `移除标签 ${tag}`);
        chip.addEventListener('click', () => { tags = tags.filter((value) => value !== tag); sync(); input.focus(); });
        return chip;
      }));
    };
    const add = () => {
      const tag = input.value.trim().replace(/[,，]$/, '').trim();
      if (tag && !tags.some((value) => value.toLocaleLowerCase() === tag.toLocaleLowerCase())) tags.push(tag);
      input.value = '';
      sync();
    };
    input?.addEventListener('keydown', (event) => {
      if (event.key === 'Enter' || event.key === ',' || event.key === '，') { event.preventDefault(); add(); }
      if (event.key === 'Backspace' && !input.value && tags.length) { tags.pop(); sync(); }
    });
    input?.addEventListener('change', add);
    box.closest('form')?.addEventListener('submit', add);
    sync();
  });

  const postForm = document.querySelector('#post-form');
  postForm?.addEventListener('submit', (event) => {
    const title = postForm.querySelector('input[name="title"]');
    const error = postForm.querySelector('[data-info-error]');
    if (!title?.value.trim()) {
      event.preventDefault();
      openInfo();
      error.hidden = false;
      title?.focus();
    }
  });

  const input = document.querySelector('#markdown-input');
  const preview = document.querySelector('#markdown-preview');
  if (!input || !preview) return;
  let timer;
  const render = async () => {
    const body = new URLSearchParams({ markdown: input.value, csrf: input.dataset.csrf });
    try {
      const response = await fetch('/admin/preview', { method: 'POST', headers: {'Content-Type': 'application/x-www-form-urlencoded'}, body });
      preview.innerHTML = response.ok ? await response.text() : '<p>预览失败</p>';
    } catch (_) { preview.innerHTML = '<p>预览暂不可用</p>'; }
  };
  input.addEventListener('input', () => { clearTimeout(timer); timer = setTimeout(render, 250); });
  const count = document.querySelector('[data-editor-count]');
  const syncCount = () => { if (count) count.textContent = `字数：${Array.from(input.value.trim()).length}`; };
  input.addEventListener('input', syncCount);
  syncCount();

  const insertMarkdown = (before, after = before, placeholder = '文本') => {
    const start = input.selectionStart;
    const end = input.selectionEnd;
    const selected = input.value.slice(start, end) || placeholder;
    input.setRangeText(`${before}${selected}${after}`, start, end, 'end');
    input.focus();
    input.dispatchEvent(new Event('input'));
  };
  document.querySelectorAll('[data-md-wrap]').forEach((button) => button.addEventListener('click', () => insertMarkdown(button.dataset.mdWrap)));
  document.querySelectorAll('[data-md-prefix]').forEach((button) => button.addEventListener('click', () => insertMarkdown(button.dataset.mdPrefix, '', '')));
  document.querySelector('[data-md-link]')?.addEventListener('click', () => insertMarkdown('[', '](https://)', '链接文字'));
  render();

  const upload = document.querySelector('#image-upload');
  const status = document.querySelector('#upload-status');
  upload?.addEventListener('change', async () => {
    if (!upload.files.length) return;
    status.textContent = '正在上传…';
    const data = new FormData(); data.append('image', upload.files[0]);
    try {
      const response = await fetch('/admin/upload', { method: 'POST', headers: {'X-CSRF-Token': input.dataset.csrf}, body: data });
      if (!response.ok) throw new Error(await response.text());
      const result = await response.json();
      const text = `![图片描述](${result.url})`;
      input.setRangeText(text, input.selectionStart, input.selectionEnd, 'end');
      input.dispatchEvent(new Event('input')); status.textContent = '上传成功，链接已插入';
    } catch (error) { status.textContent = `上传失败：${error.message}`; }
    upload.value = '';
  });
})();
