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
  const infoOpenedDirectly = infoLayer?.dataset.infoDirect === 'true';
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
  const hideInfo = () => {
    if (!infoLayer) return;
    infoLayer.classList.remove('is-open');
    infoLayer.setAttribute('aria-hidden', 'true');
  };
  const closeComposer = () => {
    if (!composerLayer) return;
    hideInfo();
    composerLayer.classList.remove('is-open');
    composerLayer.setAttribute('aria-hidden', 'true');
    document.body.classList.remove('composer-open');
    if (new URLSearchParams(window.location.search).has('compose')) history.replaceState({}, '', '/admin/posts');
  };
  const closeInfo = () => {
    if (infoOpenedDirectly) { closeComposer(); return; }
    hideInfo();
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
  if (infoLayer?.classList.contains('is-open')) window.setTimeout(() => infoLayer.querySelector('input[name="title"]')?.focus(), 180);
  document.addEventListener('keydown', (event) => {
    if (event.key !== 'Escape') return;
    if (document.querySelector('[data-cover-upload-dialog]:not([hidden])')) return;
    if (document.querySelector('[data-cover-select-layer].is-open')) return;
    if (infoLayer?.classList.contains('is-open')) closeInfo();
    else if (composerLayer?.classList.contains('is-open')) closeComposer();
  });

  const imageLightbox = document.querySelector('[data-image-lightbox]');
  if (imageLightbox) {
    const lightboxImage = imageLightbox.querySelector('[data-image-lightbox-image]');
    const lightboxCaption = imageLightbox.querySelector('[data-image-lightbox-caption]');
    const closeButton = imageLightbox.querySelector('[data-image-lightbox-close]');
    let triggerImage;
    const closeLightbox = () => {
      imageLightbox.hidden = true;
      document.body.classList.remove('image-preview-open');
      lightboxImage.removeAttribute('src');
      triggerImage?.focus();
    };
    const openLightbox = (image) => {
      triggerImage = image;
      lightboxImage.src = image.currentSrc || image.src;
      lightboxImage.alt = image.alt;
      lightboxCaption.textContent = image.alt;
      lightboxCaption.hidden = !image.alt;
      imageLightbox.hidden = false;
      document.body.classList.add('image-preview-open');
      closeButton.focus();
    };
    document.querySelectorAll('.article-page .markdown-body img').forEach((image) => {
      image.tabIndex = 0;
      image.setAttribute('role', 'button');
      image.setAttribute('aria-label', image.alt ? `放大预览：${image.alt}` : '放大预览图片');
      image.addEventListener('click', (event) => { event.preventDefault(); openLightbox(image); });
      image.addEventListener('keydown', (event) => {
        if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); openLightbox(image); }
      });
    });
    closeButton.addEventListener('click', closeLightbox);
    imageLightbox.addEventListener('click', (event) => { if (event.target === imageLightbox) closeLightbox(); });
    document.addEventListener('keydown', (event) => {
      if (event.key === 'Escape' && !imageLightbox.hidden) closeLightbox();
    });
  }

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

  const coverSelectLayer = document.querySelector('[data-cover-select-layer]');
  const coverOptions = coverSelectLayer?.querySelector('[data-post-cover-options]');
  const currentCover = document.querySelector('[data-current-cover]');
  const syncCurrentCover = () => {
    if (!currentCover || !coverOptions) return;
    const selected = coverOptions.querySelector('input[name="cover_id"]:checked');
    const source = selected?.closest('.post-cover-choice')?.querySelector('img')?.src;
    let image = currentCover.querySelector('[data-current-cover-image]');
    let empty = currentCover.querySelector('[data-current-cover-empty]');
    if (source) {
      if (!image) {
        image = document.createElement('img');
        image.dataset.currentCoverImage = '';
        image.alt = '当前文章封面';
        currentCover.append(image);
      }
      image.src = source;
      image.hidden = false;
      empty?.remove();
    } else {
      image?.removeAttribute('src');
      if (image) image.hidden = true;
      if (!empty) {
        empty = document.createElement('span');
        empty.dataset.currentCoverEmpty = '';
        empty.textContent = '暂无封面';
        currentCover.append(empty);
      }
    }
  };
  if (coverSelectLayer && coverOptions) {
    let previousCoverID = '';
    const closeCoverSelect = (restore) => {
      if (restore) {
        coverOptions.querySelectorAll('input[name="cover_id"]').forEach((radio) => { radio.checked = radio.value === previousCoverID; });
      }
      coverSelectLayer.classList.remove('is-open');
      coverSelectLayer.setAttribute('aria-hidden', 'true');
      document.body.classList.remove('cover-select-open');
      document.querySelector('[data-cover-select-open]')?.focus();
    };
    document.querySelector('[data-cover-select-open]')?.addEventListener('click', () => {
      previousCoverID = coverOptions.querySelector('input[name="cover_id"]:checked')?.value || '';
      coverSelectLayer.classList.add('is-open');
      coverSelectLayer.setAttribute('aria-hidden', 'false');
      document.body.classList.add('cover-select-open');
      window.setTimeout(() => coverOptions.querySelector('input:checked')?.focus() || coverOptions.querySelector('input')?.focus(), 50);
    });
    coverSelectLayer.querySelectorAll('[data-cover-select-cancel]').forEach((button) => button.addEventListener('click', () => closeCoverSelect(true)));
    coverSelectLayer.querySelector('[data-cover-select-confirm]')?.addEventListener('click', () => { syncCurrentCover(); closeCoverSelect(false); });
    coverSelectLayer.addEventListener('keydown', (event) => {
      if (event.key === 'Escape') { event.stopPropagation(); closeCoverSelect(true); }
    });
  }

  const coverDialog = document.querySelector('[data-cover-upload-dialog]');
  if (coverDialog) {
    const form = coverDialog.querySelector('[data-cover-upload-form]');
    const fileInput = form.querySelector('[data-cover-file]');
    const urlInput = form.querySelector('[data-cover-url]');
    const dropZone = form.querySelector('[data-cover-drop-zone]');
    const previewImage = form.querySelector('[data-cover-preview]');
    const previewLoading = form.querySelector('[data-cover-preview-loading]');
    const errorBox = form.querySelector('[data-cover-upload-error]');
    const submitButton = form.querySelector('[data-cover-upload-submit]');
    let pastedFile;
    let previewURL;
    let previewValid = false;
    let previewTimer;
    let opener;
    const clearPreview = () => {
      previewValid = false;
      previewImage.onload = null;
      previewImage.onerror = null;
      previewImage.hidden = true;
      previewImage.removeAttribute('src');
      previewLoading.hidden = true;
      dropZone.classList.remove('has-image', 'is-loading');
    };
    const releasePreviewURL = () => {
      if (previewURL) URL.revokeObjectURL(previewURL);
      previewURL = undefined;
    };
    const showPreview = (source, failureMessage) => {
      clearPreview();
      errorBox.hidden = true;
      previewLoading.hidden = false;
      dropZone.classList.add('is-loading');
      previewImage.onload = () => {
        previewValid = true;
        previewLoading.hidden = true;
        previewImage.hidden = false;
        dropZone.classList.remove('is-loading');
        dropZone.classList.add('has-image');
      };
      previewImage.onerror = () => {
        clearPreview();
        errorBox.textContent = failureMessage;
        errorBox.hidden = false;
      };
      previewImage.src = source;
    };
    const resetCoverForm = () => {
      clearTimeout(previewTimer);
      form.reset();
      pastedFile = undefined;
      clearPreview();
      releasePreviewURL();
      errorBox.hidden = true;
      errorBox.textContent = '';
    };
    const closeCoverDialog = () => {
      coverDialog.hidden = true;
      document.body.classList.remove('cover-upload-open');
      resetCoverForm();
      opener?.focus();
    };
    const openCoverDialog = (button) => {
      opener = button;
      coverDialog.hidden = false;
      document.body.classList.add('cover-upload-open');
      window.setTimeout(() => dropZone.focus(), 50);
    };
    const showFile = (file) => {
      if (!file?.type.startsWith('image/')) {
        errorBox.textContent = '仅支持 PNG、JPEG、GIF 或 WebP 图片';
        errorBox.hidden = false;
        return;
      }
      pastedFile = file;
      urlInput.value = '';
      releasePreviewURL();
      previewURL = URL.createObjectURL(file);
      showPreview(previewURL, '无法预览这张图片，请重新选择');
    };
    document.querySelectorAll('[data-cover-upload-open]').forEach((button) => button.addEventListener('click', () => openCoverDialog(button)));
    coverDialog.querySelectorAll('[data-cover-upload-close]').forEach((button) => button.addEventListener('click', closeCoverDialog));
    fileInput.addEventListener('change', () => showFile(fileInput.files?.[0]));
    urlInput.addEventListener('input', () => {
      clearTimeout(previewTimer);
      fileInput.value = '';
      pastedFile = undefined;
      releasePreviewURL();
      clearPreview();
      errorBox.hidden = true;
      const raw = urlInput.value.trim();
      if (!raw) return;
      previewTimer = window.setTimeout(() => {
        try {
          const parsed = new URL(raw);
          if (!['http:', 'https:'].includes(parsed.protocol)) throw new Error();
          showPreview(parsed.href, '图片 URL 无法加载，请检查链接是否可直接访问');
        } catch (_) {
          errorBox.textContent = '请输入有效的 HTTP 或 HTTPS 图片链接';
          errorBox.hidden = false;
        }
      }, 350);
    });
    dropZone.addEventListener('paste', (event) => {
      const file = Array.from(event.clipboardData?.items || [])
        .find((item) => item.kind === 'file' && item.type.startsWith('image/'))?.getAsFile();
      if (file) {
        event.preventDefault();
        showFile(file);
        return;
      }
      const text = event.clipboardData?.getData('text/plain')?.trim();
      if (/^https?:\/\//i.test(text || '')) {
        event.preventDefault();
        urlInput.value = text;
        urlInput.dispatchEvent(new Event('input'));
      }
    });
    coverDialog.addEventListener('keydown', (event) => {
      if (event.key === 'Escape') { event.stopPropagation(); closeCoverDialog(); }
    });
    form.addEventListener('submit', async (event) => {
      event.preventDefault();
      const data = new FormData(form);
      if (pastedFile && !fileInput.files?.length) data.set('image', pastedFile, pastedFile.name || 'clipboard.png');
      if (!data.get('image')?.size && !String(data.get('external_url') || '').trim()) {
        errorBox.textContent = '请选择图片、粘贴剪贴板图片，或填写图片 URL';
        errorBox.hidden = false;
        return;
      }
      if (!previewValid) {
        errorBox.textContent = '请等待图片预览成功后再上传';
        errorBox.hidden = false;
        return;
      }
      submitButton.disabled = true;
      submitButton.textContent = '正在上传…';
      errorBox.hidden = true;
      try {
        const response = await fetch('/admin/covers', {
          method: 'POST',
          headers: { 'Accept': 'application/json', 'X-CSRF-Token': form.querySelector('[name="csrf"]').value },
          body: data
        });
        if (!response.ok) throw new Error((await response.text()).trim());
        const cover = await response.json();
        if (form.dataset.coverUploadMode === 'library') {
          window.location.assign('/admin/covers?saved=1');
          return;
        }
        const label = document.createElement('label');
        label.className = 'post-cover-choice';
        const radio = document.createElement('input');
        radio.type = 'radio';
        radio.name = 'cover_id';
        radio.value = cover.ID;
        radio.checked = true;
        const frame = document.createElement('span');
        const image = document.createElement('img');
        image.src = cover.URL;
        image.alt = '';
        frame.append(image);
        label.append(radio, frame);
        coverOptions?.prepend(label);
        coverSelectLayer?.querySelector('[data-cover-select-empty]')?.remove();
        if (!coverSelectLayer?.classList.contains('is-open')) syncCurrentCover();
        closeCoverDialog();
      } catch (error) {
        errorBox.textContent = error.message || '上传失败，请稍后重试';
        errorBox.hidden = false;
      } finally {
        submitButton.disabled = false;
        submitButton.textContent = '上传封面';
      }
    });
  }

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
  const uploadImage = async (file) => {
    const data = new FormData();
    data.append('image', file);
    const response = await fetch('/admin/upload', { method: 'POST', headers: {'X-CSRF-Token': input.dataset.csrf}, body: data });
    if (!response.ok) throw new Error((await response.text()).trim());
    return (await response.json()).url;
  };
  const uploadAndInsertImages = async (files) => {
    if (!files.length) return;
    const start = input.selectionStart;
    const end = input.selectionEnd;
    if (status) status.textContent = files.length > 1 ? `正在上传 ${files.length} 张图片…` : '正在上传图片…';
    try {
      const urls = [];
      for (const file of files) urls.push(await uploadImage(file));
      const markdown = urls.map((url) => `![图片描述](${url})`).join('\n\n');
      input.setRangeText(markdown, start, end, 'end');
      input.focus();
      input.dispatchEvent(new Event('input'));
      if (status) status.textContent = files.length > 1 ? `${files.length} 张图片已上传并插入` : '上传成功，链接已插入';
    } catch (error) {
      if (status) status.textContent = `上传失败：${error.message}`;
    }
  };
  upload?.addEventListener('change', async () => {
    await uploadAndInsertImages(Array.from(upload.files || []));
    upload.value = '';
  });
  input.addEventListener('paste', (event) => {
    const clipboard = event.clipboardData;
    let files = Array.from(clipboard?.items || [])
      .filter((item) => item.kind === 'file' && item.type.startsWith('image/'))
      .map((item) => item.getAsFile())
      .filter(Boolean);
    if (!files.length) files = Array.from(clipboard?.files || []).filter((file) => file.type.startsWith('image/'));
    if (!files.length) return;
    event.preventDefault();
    uploadAndInsertImages(files);
  });
})();
