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
