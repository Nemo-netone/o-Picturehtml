(() => {
  'use strict';

  const STORAGE_KEYS = {
    apiConfigs: 'img_gen_api_configs',
    activeApi: 'img_gen_active_api',
    promptHistory: 'img_gen_prompt_history',
    imageParams: 'img_gen_image_params',
    autoDownload: 'img_gen_auto_download',
    guideShown: 'img_gen_guide_shown',
  };

  const DB_NAME = 'img-gen-gallery';
  const DB_VERSION = 1;
  const STORE_NAME = 'records';
  const MAX_PROMPT_HISTORY = 20;
  const GENERATION_DELAY_MS = 5000;
  const RETRY_DELAY_MS = 5000;
  const BACKGROUND_MODE_BLOCK_SELECTOR = [
    'button',
    'a',
    'input',
    'textarea',
    'select',
    'label',
    '[role="button"]',
    '[contenteditable="true"]',
    '.top-tabs',
    '.card',
    '.side-panel',
    '.data-manager-card',
    '.gallery-control-panel',
    '.gallery-card',
    '.network-status',
    '.preview-overlay',
  ].join(',');

  const DEFAULT_PICTURE_CONFIG = {
    id: 'default-picture-newapi',
    name: 'Picture NewAPI',
    baseUrl: 'https://sub.codexakihito.xyz',
    apiKey: '',
    model: 'gpt-image-2',
    availableModels: ['gpt-image-2'],
  };

  const DEFAULT_CONFIGS = [
    DEFAULT_PICTURE_CONFIG,
    {
      id: 'default-openai',
      name: 'OpenAI 官方',
      baseUrl: 'https://api.openai.com',
      apiKey: '',
      model: 'dall-e-3',
      availableModels: [],
    },
    {
      id: 'default-custom',
      name: '自定义 API（示例）',
      baseUrl: 'https://your-api-endpoint.com',
      apiKey: '',
      model: 'dall-e-3',
      availableModels: [],
    },
  ];

  const $ = (selector, root = document) => root.querySelector(selector);
  const $$ = (selector, root = document) => Array.from(root.querySelectorAll(selector));
  const byId = (id) => document.getElementById(id);

  const dom = {};
  const state = {
    apiConfigs: [],
    activeApiId: null,
    editingConfigId: null,
    fetchedModels: [],
    modelIds: [],
    selectedModel: '',
    selectedGenCount: 1,
    refImages: [],
    promptHistory: [],
    gallery: [],
    imageParams: {
      size: '1024x1024',
      quality: 'standard',
      style: 'natural',
    },
    autoDownload: false,
    generation: {
      active: false,
      cancelRequested: false,
      total: 0,
      done: 0,
      success: 0,
      failed: 0,
      startTime: 0,
      timer: null,
      abortController: null,
    },
    galleryView: {
      displayMode: 'card',
      sortMode: 'time-desc',
      groupByMode: false,
      groupByContent: false,
      activeFilters: new Set(),
    },
    preview: {
      index: 0,
      urlMode: false,
      scale: 1,
      panX: 0,
      panY: 0,
      dragging: false,
      dragStartX: 0,
      dragStartY: 0,
      panStartX: 0,
      panStartY: 0,
    },
    network: {
      isOnline: navigator.onLine,
      lastCheck: null,
      latency: null,
      checking: false,
    },
  };

  let dbPromise = null;

  document.addEventListener('DOMContentLoaded', init);

  async function init() {
    cacheDom();
    bindEvents();
    loadApiConfigs();
    loadPromptHistory();
    loadImageParams();
    loadAutoDownloadSetting();
    initGuideBox();
    createParticles();
    updateNetworkStatusDisplay();
    renderAll();

    try {
      await loadGallery();
    } catch (error) {
      showStatus('err', `展馆加载失败：${error.message}`);
    }

    renderGalleryWithControls();
    updateStorageInfo();
    repairRemoteGalleryRecords().catch((error) => {
      appendEvent('event', `图库旧记录修复失败：${error.message}`);
    });
  }

  function cacheDom() {
    Object.assign(dom, {
      topTabs: $$('.top-tab'),
      tabDraw: byId('tabDraw'),
      tabGallery: byId('tabGallery'),
      topGalleryBadge: byId('topGalleryBadge'),
      apiConfigList: byId('apiConfigList'),
      toggleApiManager: byId('toggleApiManager'),
      toggleIcon: byId('toggleIcon'),
      apiManagerPanel: byId('apiManagerPanel'),
      addNewApiConfig: byId('addNewApiConfig'),
      saveApiConfig: byId('saveApiConfig'),
      cancelApiConfig: byId('cancelApiConfig'),
      newConfigName: byId('newConfigName'),
      newBaseUrl: byId('newBaseUrl'),
      newApiKey: byId('newApiKey'),
      newPwdToggle: byId('newPwdToggle'),
      newModel: byId('newModel'),
      newModelPanel: byId('newModelPanel'),
      fetchModelsBtn: byId('fetchModelsBtn'),
      modelListDisplay: byId('modelListDisplay'),
      modelListContent: byId('modelListContent'),
      currentApiName: byId('currentApiName'),
      baseUrl: byId('baseUrl'),
      apiKey: byId('apiKey'),
      pwdToggle: byId('pwdToggle'),
      model: byId('model'),
      modelPanel: byId('modelPanel'),
      imageFile: byId('imageFile'),
      thumbRow: byId('thumbRow'),
      thumbAdd: byId('thumbAdd'),
      imageSize: byId('imageSize'),
      imageQuality: byId('imageQuality'),
      imageStyle: byId('imageStyle'),
      customSizePanel: byId('customSizePanel'),
      customWidth: byId('customWidth'),
      customHeight: byId('customHeight'),
      prompt: byId('prompt'),
      promptHistoryBtn: byId('promptHistoryBtn'),
      promptHistoryPanel: byId('promptHistoryPanel'),
      promptHistoryList: byId('promptHistoryList'),
      promptHistoryEmpty: byId('promptHistoryEmpty'),
      promptHistoryCount: byId('promptHistoryCount'),
      genBtn: byId('genBtn'),
      genProgress: byId('genProgress'),
      progressText: byId('progressText'),
      progressBar: byId('progressBar'),
      progressSuccess: byId('progressSuccess'),
      progressFailed: byId('progressFailed'),
      progressRemaining: byId('progressRemaining'),
      cancelGenBtn: byId('cancelGenBtn'),
      loadingMini: byId('loadingMini'),
      statEvents: byId('statEvents'),
      statTextLen: byId('statTextLen'),
      statElapsed: byId('statElapsed'),
      eventLog: byId('eventLog'),
      textStream: byId('textStream'),
      resultArea: byId('resultArea'),
      resultGrid: byId('resultGrid'),
      statusBar: byId('statusBar'),
      galleryGrid: byId('galleryGrid'),
      galleryCount: byId('galleryCount'),
      galleryEmpty: byId('galleryEmpty'),
      text2imgCount: byId('text2imgCount'),
      img2imgCount: byId('img2imgCount'),
      totalCount: byId('totalCount'),
      sortMode: byId('sortMode'),
      groupByMode: byId('groupByMode'),
      groupByContent: byId('groupByContent'),
      tagFilters: byId('tagFilters'),
      toggleDataManager: byId('toggleDataManager'),
      dataManagerContent: byId('dataManagerContent'),
      storageImageCount: byId('storageImageCount'),
      storageSize: byId('storageSize'),
      storageApiCount: byId('storageApiCount'),
      storagePromptCount: byId('storagePromptCount'),
      exportAllDataBtn: byId('exportAllDataBtn'),
      importDataBtn: byId('importDataBtn'),
      downloadAllImagesBtn: byId('downloadAllImagesBtn'),
      clearAllDataBtn: byId('clearAllDataBtn'),
      autoDownloadCheckbox: byId('autoDownloadCheckbox'),
      importFileInput: byId('importFileInput'),
      networkStatusText: byId('networkStatusText'),
      networkStatusDot: byId('networkStatusDot'),
      connectionStatus: byId('connectionStatus'),
      networkLatency: byId('networkLatency'),
      lastCheckTime: byId('lastCheckTime'),
      quickTestBtn: byId('quickTestBtn'),
      fullDiagBtn: byId('fullDiagBtn'),
      diagResult: byId('diagResult'),
      networkStatusValue: byId('networkStatusValue'),
      networkPing: byId('networkPing'),
      previewOverlay: byId('previewOverlay'),
      previewImg: byId('previewImg'),
      previewNavPrev: byId('previewNavPrev'),
      previewNavNext: byId('previewNavNext'),
      previewCounter: byId('previewCounter'),
      closeGuide: byId('closeGuide'),
      guideBox: byId('guideBox'),
      particles: byId('particles'),
    });
  }

  function bindEvents() {
    dom.topTabs.forEach((tab) => {
      tab.addEventListener('click', () => switchTab(tab.dataset.page));
    });

    dom.toggleApiManager?.addEventListener('click', toggleApiManager);
    dom.addNewApiConfig?.addEventListener('click', startAddConfig);
    dom.cancelApiConfig?.addEventListener('click', closeApiManager);
    dom.saveApiConfig?.addEventListener('click', saveConfigFromForm);
    dom.fetchModelsBtn?.addEventListener('click', fetchModelsForForm);
    dom.newPwdToggle?.addEventListener('click', () => togglePassword(dom.newApiKey, dom.newPwdToggle));
    dom.pwdToggle?.addEventListener('click', () => togglePassword(dom.apiKey, dom.pwdToggle));

    bindCombo(dom.newModel, dom.newModelPanel, () => state.fetchedModels, () => {
      renderModelList();
    });

    dom.thumbAdd?.addEventListener('click', () => dom.imageFile?.click());
    dom.imageFile?.addEventListener('change', () => {
      if (dom.imageFile.files?.length) addRefFiles(dom.imageFile.files);
      dom.imageFile.value = '';
    });

    dom.imageSize?.addEventListener('change', () => {
      const isCustom = dom.imageSize.value === 'custom';
      dom.customSizePanel.hidden = !isCustom;
      if (isCustom) dom.customWidth?.focus();
      saveImageParams();
    });
    [dom.imageQuality, dom.imageStyle, dom.customWidth, dom.customHeight].forEach((input) => {
      input?.addEventListener('change', saveImageParams);
    });

    $$('.example-btn').forEach((button) => {
      button.addEventListener('click', () => {
        dom.prompt.value = button.dataset.prompt || '';
        dom.prompt.focus();
      });
    });

    dom.promptHistoryBtn?.addEventListener('click', () => {
      dom.promptHistoryPanel.classList.toggle('open');
    });

    $$('.gen-count-btn').forEach((button) => {
      button.addEventListener('click', () => {
        $$('.gen-count-btn').forEach((item) => item.classList.remove('active'));
        button.classList.add('active');
        state.selectedGenCount = Number(button.dataset.count || 1);
      });
    });

    dom.genBtn?.addEventListener('click', generate);
    dom.cancelGenBtn?.addEventListener('click', cancelGeneration);
    dom.prompt?.addEventListener('keydown', (event) => {
      if ((event.ctrlKey || event.metaKey) && event.key === 'Enter') {
        event.preventDefault();
        generate();
      }
    });

    $$('input[name="displayMode"]').forEach((radio) => {
      radio.addEventListener('change', () => {
        state.galleryView.displayMode = radio.value;
        renderGalleryWithControls();
      });
    });
    dom.sortMode?.addEventListener('change', () => {
      state.galleryView.sortMode = dom.sortMode.value;
      renderGalleryWithControls();
    });
    dom.groupByMode?.addEventListener('change', () => {
      state.galleryView.groupByMode = dom.groupByMode.checked;
      renderGalleryWithControls();
    });
    dom.groupByContent?.addEventListener('change', () => {
      state.galleryView.groupByContent = dom.groupByContent.checked;
      renderGalleryWithControls();
    });
    $$('.tag-filter').forEach((button) => {
      button.addEventListener('click', () => {
        const tag = button.dataset.tag;
        if (state.galleryView.activeFilters.has(tag)) {
          state.galleryView.activeFilters.delete(tag);
          button.classList.remove('active');
        } else {
          state.galleryView.activeFilters.add(tag);
          button.classList.add('active');
        }
        renderGalleryWithControls();
      });
    });

    dom.toggleDataManager?.addEventListener('click', () => {
      dom.dataManagerContent.classList.toggle('collapsed');
      dom.toggleDataManager.classList.toggle('collapsed');
    });
    dom.exportAllDataBtn?.addEventListener('click', exportAllData);
    dom.importDataBtn?.addEventListener('click', () => dom.importFileInput?.click());
    dom.importFileInput?.addEventListener('change', importAllData);
    dom.downloadAllImagesBtn?.addEventListener('click', downloadAllImages);
    dom.clearAllDataBtn?.addEventListener('click', clearAllData);
    dom.autoDownloadCheckbox?.addEventListener('change', () => {
      state.autoDownload = false;
      dom.autoDownloadCheckbox.checked = true;
      saveAutoDownloadSetting();
      showStatus('info', '生成图片会保存在浏览器图库，不会自动下载到电脑');
    });

    dom.quickTestBtn?.addEventListener('click', quickNetworkTest);
    dom.fullDiagBtn?.addEventListener('click', fullNetworkDiagnosis);
    window.addEventListener('online', () => {
      state.network.isOnline = true;
      updateNetworkStatusDisplay();
    });
    window.addEventListener('offline', () => {
      state.network.isOnline = false;
      updateNetworkStatusDisplay();
    });

    dom.previewOverlay?.addEventListener('click', (event) => {
      if (event.target === dom.previewOverlay || event.target.classList.contains('preview-close')) {
        closePreview();
      }
    });
    dom.previewNavPrev?.addEventListener('click', (event) => {
      event.stopPropagation();
      prevImage();
    });
    dom.previewNavNext?.addEventListener('click', (event) => {
      event.stopPropagation();
      nextImage();
    });
    dom.previewOverlay?.addEventListener('wheel', handlePreviewWheel, { passive: false });
    dom.previewImg?.addEventListener('mousedown', startPreviewDrag);
    window.addEventListener('mousemove', movePreviewDrag);
    window.addEventListener('mouseup', stopPreviewDrag);
    document.addEventListener('keydown', handleGlobalKeydown);
    document.addEventListener('dblclick', handleBackgroundModeDblClick);
    window.addEventListener('scroll', handleScroll);
  }

  function handleBackgroundModeDblClick(event) {
    if (document.body.classList.contains('bg-only')) {
      document.body.classList.remove('bg-only');
      return;
    }

    if (dom.previewOverlay?.classList.contains('open')) return;
    if (!(event.target instanceof Element)) return;
    if (event.target.closest(BACKGROUND_MODE_BLOCK_SELECTOR)) return;

    document.body.classList.add('bg-only');
  }

  function renderAll() {
    renderApiConfigs();
    updateCurrentApiDisplay();
    renderPromptHistory();
    renderThumbnails();
    renderGalleryWithControls();
    updateStorageInfo();
  }

  function switchTab(page) {
    dom.topTabs.forEach((tab) => tab.classList.toggle('active', tab.dataset.page === page));
    dom.tabDraw?.classList.toggle('active', page === 'draw');
    dom.tabGallery?.classList.toggle('active', page === 'gallery');
    if (page === 'gallery') renderGalleryWithControls();
  }

  function handleScroll() {
    $('.top-tabs')?.classList.toggle('scrolled', window.scrollY > 10);
  }

  function showStatus(type, message, timeout = 4500) {
    if (!dom.statusBar) return;
    dom.statusBar.className = `status-bar show ${type || 'info'}`;
    dom.statusBar.textContent = message;
    if (timeout > 0) {
      window.clearTimeout(showStatus._timer);
      showStatus._timer = window.setTimeout(() => {
        dom.statusBar.className = 'status-bar';
        dom.statusBar.textContent = '';
      }, timeout);
    }
  }

  function togglePassword(input, button) {
    if (!input || !button) return;
    const nextType = input.type === 'password' ? 'text' : 'password';
    input.type = nextType;
    button.textContent = nextType === 'password' ? '眼' : '隐';
  }

  function setPanelHidden(element, hidden) {
    if (element) element.hidden = hidden;
  }

  function createTextElement(tag, className, text) {
    const element = document.createElement(tag);
    if (className) element.className = className;
    element.textContent = text;
    return element;
  }

  function copyText(text, successMessage = '已复制') {
    if (!navigator.clipboard?.writeText) {
      showStatus('err', '当前浏览器不支持剪贴板写入');
      return;
    }
    navigator.clipboard.writeText(text || '').then(
      () => showStatus('done', successMessage),
      (error) => showStatus('err', `复制失败：${error.message}`),
    );
  }

  function loadApiConfigs() {
    try {
      const saved = localStorage.getItem(STORAGE_KEYS.apiConfigs);
      state.apiConfigs = saved ? JSON.parse(saved) : [];
      state.activeApiId = localStorage.getItem(STORAGE_KEYS.activeApi);
    } catch {
      state.apiConfigs = [];
      state.activeApiId = null;
    }

    if (!Array.isArray(state.apiConfigs) || state.apiConfigs.length === 0) {
      state.apiConfigs = structuredCloneSafe(DEFAULT_CONFIGS);
    }

    ensureDefaultPictureConfig();

    if (!state.apiConfigs.some((config) => config.id === state.activeApiId)) {
      state.activeApiId = state.apiConfigs[0]?.id || null;
    }

    if (!state.activeApiId || state.activeApiId === 'default-openai' || state.activeApiId === 'default-custom') {
      state.activeApiId = DEFAULT_PICTURE_CONFIG.id;
    }

    saveApiConfigs();
  }

  function ensureDefaultPictureConfig() {
    const existing = state.apiConfigs.find((config) => config.id === DEFAULT_PICTURE_CONFIG.id);
    if (existing) {
      existing.name = DEFAULT_PICTURE_CONFIG.name;
      existing.baseUrl = DEFAULT_PICTURE_CONFIG.baseUrl;
      existing.model = DEFAULT_PICTURE_CONFIG.model;
      existing.availableModels = [...DEFAULT_PICTURE_CONFIG.availableModels];
      return;
    }

    state.apiConfigs.unshift(structuredCloneSafe(DEFAULT_PICTURE_CONFIG));
  }

  function saveApiConfigs() {
    try {
      localStorage.setItem(STORAGE_KEYS.apiConfigs, JSON.stringify(state.apiConfigs));
      if (state.activeApiId) localStorage.setItem(STORAGE_KEYS.activeApi, state.activeApiId);
      updateStorageInfo();
    } catch (error) {
      showStatus('err', `保存 API 配置失败：${error.message}`);
    }
  }

  function renderApiConfigs() {
    if (!dom.apiConfigList) return;
    dom.apiConfigList.innerHTML = '';

    state.apiConfigs.forEach((config) => {
      const item = document.createElement('div');
      item.className = `api-config-item${config.id === state.activeApiId ? ' active' : ''}`;

      const info = document.createElement('div');
      info.className = 'api-config-info';
      info.appendChild(createTextElement('div', 'api-config-name', config.name || '未命名配置'));

      const details = document.createElement('div');
      details.className = 'api-config-details';
      details.appendChild(createTextElement('span', 'api-config-detail', `URL: ${config.baseUrl || '-'}`));
      details.appendChild(createTextElement('span', 'api-config-detail', `Model: ${config.model || '未设置'}`));
      details.appendChild(createTextElement('span', 'api-config-detail', config.apiKey ? 'Key: 已填写' : 'Key: 未填写'));
      info.appendChild(details);

      const actions = document.createElement('div');
      actions.className = 'api-config-actions';

      const useBtn = createConfigButton(config.id === state.activeApiId ? '使用中' : '启用', () => activateApiConfig(config.id));
      if (config.id === state.activeApiId) useBtn.classList.add('active-btn');
      actions.appendChild(useBtn);
      actions.appendChild(createConfigButton('复制 URL', () => copyText(config.baseUrl, 'Base URL 已复制')));
      actions.appendChild(createConfigButton('复制 Key', () => copyText(config.apiKey, 'API Key 已复制')));
      actions.appendChild(createConfigButton('编辑', () => editApiConfig(config.id)));
      const delBtn = createConfigButton('删除', () => deleteApiConfig(config.id));
      delBtn.classList.add('delete-btn');
      actions.appendChild(delBtn);

      item.append(info, actions);
      dom.apiConfigList.appendChild(item);
    });
  }

  function createConfigButton(text, onClick) {
    const button = document.createElement('button');
    button.type = 'button';
    button.className = 'api-config-btn';
    button.textContent = text;
    button.addEventListener('click', (event) => {
      event.stopPropagation();
      onClick();
    });
    return button;
  }

  function activateApiConfig(id) {
    state.activeApiId = id;
    saveApiConfigs();
    renderApiConfigs();
    updateCurrentApiDisplay();
    showStatus('done', '已切换 API 配置');
  }

  function updateCurrentApiDisplay() {
    const config = getActiveConfig();
    if (config) {
      dom.currentApiName.textContent = config.apiKey ? config.name : `${config.name}（未填写 API Key）`;
      dom.currentApiName.style.color = config.apiKey ? 'var(--success)' : 'var(--warning)';
      dom.baseUrl.value = config.baseUrl || '';
      dom.apiKey.value = config.apiKey || '';
      dom.model.value = config.model || '';
      state.modelIds = Array.isArray(config.availableModels) ? config.availableModels : [];
      return;
    }

    dom.currentApiName.textContent = '未选择';
    dom.currentApiName.style.color = 'var(--warning)';
    dom.baseUrl.value = '';
    dom.apiKey.value = '';
    dom.model.value = '';
    state.modelIds = [];
  }

  function getActiveConfig() {
    return state.apiConfigs.find((config) => config.id === state.activeApiId) || null;
  }

  function toggleApiManager() {
    const nextHidden = !dom.apiManagerPanel.hidden;
    setPanelHidden(dom.apiManagerPanel, nextHidden);
    dom.toggleIcon.textContent = nextHidden ? '▼' : '▲';
  }

  function startAddConfig() {
    state.editingConfigId = null;
    state.fetchedModels = [];
    state.selectedModel = '';
    dom.newConfigName.value = '';
    dom.newBaseUrl.value = '';
    dom.newApiKey.value = '';
    dom.newModel.value = '';
    setPanelHidden(dom.apiManagerPanel, false);
    setPanelHidden(dom.modelListDisplay, true);
    dom.toggleIcon.textContent = '▲';
    dom.saveApiConfig.textContent = '保存配置';
    dom.newConfigName.focus();
  }

  function closeApiManager() {
    state.editingConfigId = null;
    state.fetchedModels = [];
    state.selectedModel = '';
    setPanelHidden(dom.apiManagerPanel, true);
    setPanelHidden(dom.modelListDisplay, true);
    dom.toggleIcon.textContent = '▼';
    dom.saveApiConfig.textContent = '保存配置';
  }

  function editApiConfig(id) {
    const config = state.apiConfigs.find((item) => item.id === id);
    if (!config) return;
    state.editingConfigId = id;
    state.fetchedModels = Array.isArray(config.availableModels) ? [...config.availableModels] : [];
    state.selectedModel = config.model || '';
    dom.newConfigName.value = config.name || '';
    dom.newBaseUrl.value = config.baseUrl || '';
    dom.newApiKey.value = config.apiKey || '';
    dom.newModel.value = config.model || '';
    dom.saveApiConfig.textContent = '更新配置';
    setPanelHidden(dom.apiManagerPanel, false);
    setPanelHidden(dom.modelListDisplay, state.fetchedModels.length === 0);
    dom.toggleIcon.textContent = '▲';
    renderModelList();
  }

  function saveConfigFromForm() {
    const name = dom.newConfigName.value.trim();
    const apiKey = dom.newApiKey.value.trim();
    const model = dom.newModel.value.trim();
    let baseUrl = '';

    if (!name) {
      showStatus('err', '请填写配置名称');
      dom.newConfigName.focus();
      return;
    }
    try {
      baseUrl = normalizeBaseUrl(dom.newBaseUrl.value);
    } catch (error) {
      showStatus('err', error.message);
      dom.newBaseUrl.focus();
      return;
    }
    if (!apiKey) {
      showStatus('err', '请填写 API Key');
      dom.newApiKey.focus();
      return;
    }
    if (!model) {
      showStatus('err', '请填写或选择 Model');
      dom.newModel.focus();
      return;
    }

    const config = {
      id: state.editingConfigId || `api-${Date.now()}`,
      name,
      baseUrl,
      apiKey,
      model,
      availableModels: state.fetchedModels.length ? [...state.fetchedModels] : [],
    };

    const index = state.apiConfigs.findIndex((item) => item.id === config.id);
    if (index >= 0) {
      state.apiConfigs[index] = config;
    } else {
      state.apiConfigs.push(config);
    }

    state.activeApiId = config.id;
    saveApiConfigs();
    renderApiConfigs();
    updateCurrentApiDisplay();
    closeApiManager();
    showStatus('done', index >= 0 ? 'API 配置已更新' : 'API 配置已保存');
  }

  function deleteApiConfig(id) {
    const config = state.apiConfigs.find((item) => item.id === id);
    if (!config) return;
    if (!confirm(`确定删除配置“${config.name}”吗？`)) return;

    state.apiConfigs = state.apiConfigs.filter((item) => item.id !== id);
    if (state.apiConfigs.length === 0) {
      state.apiConfigs = structuredCloneSafe(DEFAULT_CONFIGS);
    }
    if (state.activeApiId === id) {
      state.activeApiId = state.apiConfigs[0]?.id || null;
    }
    saveApiConfigs();
    renderApiConfigs();
    updateCurrentApiDisplay();
    showStatus('info', '配置已删除');
  }

  async function fetchModelsForForm() {
    let baseUrl = '';
    try {
      baseUrl = normalizeBaseUrl(dom.newBaseUrl.value);
    } catch (error) {
      showStatus('err', error.message);
      dom.newBaseUrl.focus();
      return;
    }
    const apiKey = dom.newApiKey.value.trim();
    if (!apiKey) {
      showStatus('err', '请先填写 API Key');
      dom.newApiKey.focus();
      return;
    }

    setPanelHidden(dom.modelListDisplay, false);
    dom.modelListContent.textContent = '正在获取模型列表...';
    dom.fetchModelsBtn.disabled = true;
    dom.fetchModelsBtn.textContent = '获取中...';

    try {
      const models = await fetchModelIds(baseUrl, apiKey);
      state.fetchedModels = models;
      if (!dom.newModel.value && models.length === 1) dom.newModel.value = models[0];
      renderModelList();
      showStatus('done', `成功获取 ${models.length} 个模型`);
    } catch (error) {
      dom.modelListContent.textContent = `获取失败：${error.message}`;
      showStatus('err', `获取模型失败：${error.message}`, 7000);
    } finally {
      dom.fetchModelsBtn.disabled = false;
      dom.fetchModelsBtn.textContent = '获取可用模型列表';
    }
  }

  async function fetchModelIds(baseUrl, apiKey) {
    const response = await fetchApiWithTimeout(baseUrl, '/v1/models', {
      method: 'GET',
      headers: {
        Authorization: `Bearer ${apiKey}`,
      },
    }, 12000);

    if (!response.ok) {
      throw new Error(`HTTP ${response.status}: ${response.statusText}`);
    }

    const data = await response.json();
    const models = Array.isArray(data?.data)
      ? data.data.map((model) => model?.id || model?.model).filter(Boolean)
      : Array.isArray(data)
        ? data.map((model) => model?.id || model?.model || model).filter(Boolean)
        : [];

    if (!models.length) throw new Error('响应中没有可用模型');
    return [...new Set(models)].sort();
  }

  function renderModelList() {
    if (!dom.modelListContent) return;
    dom.modelListContent.innerHTML = '';
    if (!state.fetchedModels.length) {
      dom.modelListContent.textContent = '暂无模型';
      return;
    }

    const currentModel = getConfigFormModel();
    state.fetchedModels.forEach((modelId) => {
      const item = createTextElement('button', 'model-item', modelId);
      item.type = 'button';
      if (currentModel === modelId) item.classList.add('selected');
      item.addEventListener('click', () => {
        dom.newModel.value = modelId;
        renderModelList();
      });
      dom.modelListContent.appendChild(item);
    });
  }

  function bindCombo(input, panel, listProvider, onPick) {
    if (!input || !panel) return;

    const render = () => {
      panel.innerHTML = '';
      const values = listProvider();
      const query = input.value.trim().toLowerCase();
      const filtered = query
        ? values.filter((value) => String(value).toLowerCase().includes(query))
        : values;

      if (!filtered.length) {
        panel.appendChild(createTextElement('div', 'combo-empty', values.length ? '没有匹配的模型' : '暂无模型列表'));
        return;
      }

      filtered.slice(0, 80).forEach((value) => {
        const item = createTextElement('div', 'combo-item', value);
        item.addEventListener('mousedown', (event) => {
          event.preventDefault();
          input.value = value;
          panel.classList.remove('open');
          onPick(value);
        });
        panel.appendChild(item);
      });
    };

    input.addEventListener('focus', () => {
      render();
      panel.classList.add('open');
    });
    input.addEventListener('input', () => {
      render();
      onPick(input.value.trim());
      panel.classList.add('open');
    });
    input.addEventListener('blur', () => {
      window.setTimeout(() => panel.classList.remove('open'), 150);
    });
  }

  function getConfigFormModel() {
    return dom.newModel?.value?.trim() || '';
  }

  function normalizeBaseUrl(input) {
    let base = String(input || '').trim();
    if (!base) throw new Error('Base URL 不能为空');
    if (!/^https?:\/\//i.test(base)) throw new Error('Base URL 必须以 http:// 或 https:// 开头');
    base = base.replace(/\/+$/, '');
    if (base.endsWith('/v1')) base = base.slice(0, -3);
    try {
      new URL(base);
    } catch {
      throw new Error('Base URL 格式不正确');
    }
    return base;
  }

  function createApiRequest(baseUrl, path, headers = {}) {
    const normalizedBaseUrl = normalizeBaseUrl(baseUrl);
    const requestHeaders = { ...headers };

    if (shouldUseApiProxy(normalizedBaseUrl)) {
      requestHeaders['X-Picture-Upstream'] = normalizedBaseUrl;
      return {
        url: `${window.location.origin}${path}`,
        headers: requestHeaders,
      };
    }

    if (window.location.protocol === 'file:') {
      throw new Error('当前页面是 file:// 直接打开，无法使用同源代理绕过 CORS。请用本地代理服务地址或部署后的 Cloudflare Pages 地址打开页面。');
    }

    return {
      url: `${normalizedBaseUrl}${path}`,
      headers: requestHeaders,
    };
  }

  function shouldUseApiProxy(baseUrl) {
    if (window.location.protocol !== 'http:' && window.location.protocol !== 'https:') return false;
    try {
      return new URL(baseUrl).origin !== window.location.origin;
    } catch {
      return false;
    }
  }

  function loadPromptHistory() {
    try {
      const saved = JSON.parse(localStorage.getItem(STORAGE_KEYS.promptHistory) || '[]');
      state.promptHistory = Array.isArray(saved) ? saved.filter(Boolean).slice(0, MAX_PROMPT_HISTORY) : [];
    } catch {
      state.promptHistory = [];
    }
  }

  function savePromptHistory() {
    try {
      localStorage.setItem(STORAGE_KEYS.promptHistory, JSON.stringify(state.promptHistory));
      updateStorageInfo();
    } catch {}
  }

  function addPromptToHistory(prompt) {
    const text = String(prompt || '').trim();
    if (!text) return;
    state.promptHistory = [text, ...state.promptHistory.filter((item) => item !== text)].slice(0, MAX_PROMPT_HISTORY);
    savePromptHistory();
    renderPromptHistory();
  }

  function renderPromptHistory() {
    if (!dom.promptHistoryList || !dom.promptHistoryEmpty) return;
    dom.promptHistoryList.innerHTML = '';
    dom.promptHistoryEmpty.hidden = state.promptHistory.length > 0;
    dom.promptHistoryCount.textContent = state.promptHistory.length ? String(state.promptHistory.length) : '';

    state.promptHistory.forEach((prompt, index) => {
      const item = document.createElement('div');
      item.className = 'prompt-history-item';
      const text = createTextElement('div', 'prompt-history-text', prompt);
      text.title = prompt;
      text.addEventListener('click', () => {
        dom.prompt.value = prompt;
        dom.promptHistoryPanel.classList.remove('open');
        dom.prompt.focus();
      });

      const pinBtn = createTextElement('button', '', '置顶');
      pinBtn.type = 'button';
      pinBtn.addEventListener('click', () => {
        state.promptHistory.splice(index, 1);
        state.promptHistory.unshift(prompt);
        savePromptHistory();
        renderPromptHistory();
      });

      const delBtn = createTextElement('button', 'danger', '删除');
      delBtn.type = 'button';
      delBtn.addEventListener('click', () => {
        state.promptHistory.splice(index, 1);
        savePromptHistory();
        renderPromptHistory();
      });

      item.append(text, pinBtn, delBtn);
      dom.promptHistoryList.appendChild(item);
    });
  }

  function loadImageParams() {
    try {
      const saved = JSON.parse(localStorage.getItem(STORAGE_KEYS.imageParams) || '{}');
      state.imageParams = { ...state.imageParams, ...saved };
    } catch {}
    dom.imageSize.value = state.imageParams.size || '1024x1024';
    dom.imageQuality.value = state.imageParams.quality || 'standard';
    dom.imageStyle.value = state.imageParams.style || 'natural';
    if (state.imageParams.size?.includes('x') && !['1024x1024', '1792x1024', '1024x1792', '1440x1440'].includes(state.imageParams.size)) {
      const [width, height] = state.imageParams.size.split('x');
      dom.imageSize.value = 'custom';
      dom.customWidth.value = width || '';
      dom.customHeight.value = height || '';
    }
    dom.customSizePanel.hidden = dom.imageSize.value !== 'custom';
  }

  function saveImageParams() {
    state.imageParams = getImageParams();
    try {
      localStorage.setItem(STORAGE_KEYS.imageParams, JSON.stringify(state.imageParams));
    } catch {}
  }

  function getImageParams() {
    const quality = dom.imageQuality.value || 'standard';
    const style = dom.imageStyle.value || 'natural';
    if (dom.imageSize.value === 'custom') {
      const width = normalizeImageDimension(dom.customWidth.value, 1024);
      const height = normalizeImageDimension(dom.customHeight.value, 1024);
      dom.customWidth.value = width;
      dom.customHeight.value = height;
      return { size: `${width}x${height}`, quality, style };
    }
    return { size: dom.imageSize.value || '1024x1024', quality, style };
  }

  function normalizeImageDimension(value, fallback) {
    const number = Number.parseInt(value, 10);
    const bounded = Number.isFinite(number) ? Math.min(4096, Math.max(256, number)) : fallback;
    return Math.round(bounded / 16) * 16;
  }

  async function addRefFiles(files) {
    const validFiles = Array.from(files).filter((file) => file.type.startsWith('image/'));
    if (!validFiles.length) {
      showStatus('err', '请选择图片文件');
      return;
    }

    for (const file of validFiles) {
      try {
        const compressed = await compressImage(file);
        state.refImages.push(compressed);
      } catch (error) {
        showStatus('err', `参考图处理失败：${error.message}`);
      }
    }
    renderThumbnails();
  }

  function compressImage(file, maxWidth = 1024, maxHeight = 1024, quality = 0.76) {
    return new Promise((resolve, reject) => {
      const reader = new FileReader();
      reader.onerror = () => reject(new Error('读取文件失败'));
      reader.onload = () => {
        const image = new Image();
        image.onerror = () => reject(new Error('图片解码失败'));
        image.onload = () => {
          let { width, height } = image;
          const ratio = Math.min(1, maxWidth / width, maxHeight / height);
          width = Math.round(width * ratio);
          height = Math.round(height * ratio);

          const canvas = document.createElement('canvas');
          canvas.width = width;
          canvas.height = height;
          const context = canvas.getContext('2d');
          context.drawImage(image, 0, 0, width, height);
          const dataUrl = canvas.toDataURL('image/jpeg', quality);
          resolve({
            file,
            dataUrl,
            originalSize: file.size,
            compressedSize: Math.round((dataUrl.length * 3) / 4),
          });
        };
        image.src = reader.result;
      };
      reader.readAsDataURL(file);
    });
  }

  function renderThumbnails() {
    if (!dom.thumbRow || !dom.thumbAdd) return;
    $$('.thumb-item', dom.thumbRow).forEach((item) => item.remove());

    state.refImages.forEach((ref, index) => {
      const item = document.createElement('div');
      item.className = 'thumb-item';
      const image = document.createElement('img');
      image.src = ref.dataUrl;
      image.alt = `参考图片 ${index + 1}`;
      image.addEventListener('click', () => openPreview(ref.dataUrl));
      const remove = createTextElement('button', 'thumb-remove', '×');
      remove.type = 'button';
      remove.title = '移除参考图';
      remove.addEventListener('click', (event) => {
        event.stopPropagation();
        state.refImages.splice(index, 1);
        renderThumbnails();
      });
      item.append(image, remove);
      dom.thumbRow.insertBefore(item, dom.thumbAdd);
    });
  }

  function clearRefImages() {
    state.refImages = [];
    renderThumbnails();
  }

  function lockInputs(locked) {
    [dom.prompt, dom.model, dom.imageSize, dom.imageQuality, dom.imageStyle, dom.customWidth, dom.customHeight].forEach((input) => {
      if (input) input.disabled = locked;
    });
    if (dom.thumbAdd) dom.thumbAdd.hidden = locked;
    $$('.thumb-remove').forEach((button) => {
      button.hidden = locked;
    });
  }

  function openDB() {
    if (dbPromise) return dbPromise;
    dbPromise = new Promise((resolve, reject) => {
      const request = indexedDB.open(DB_NAME, DB_VERSION);
      request.onerror = () => reject(request.error);
      request.onupgradeneeded = () => {
        const db = request.result;
        if (!db.objectStoreNames.contains(STORE_NAME)) {
          db.createObjectStore(STORE_NAME, { keyPath: 'id' });
        }
      };
      request.onsuccess = () => resolve(request.result);
    });
    return dbPromise;
  }

  async function loadGallery() {
    const db = await openDB();
    const records = await readAllRecords(db);
    state.gallery = records.sort((a, b) => new Date(b.createdAt || b.time || 0) - new Date(a.createdAt || a.time || 0));
  }

  async function repairRemoteGalleryRecords() {
    const remoteRecords = state.gallery.filter((record) => isRemoteImageSource(record.dataUrl));
    if (!remoteRecords.length) return;

    appendEvent('event', `检测到 ${remoteRecords.length} 条远程图片记录，正在尝试转存到浏览器本地图库`);
    let repaired = 0;
    let failed = 0;

    for (const record of remoteRecords) {
      try {
        const resolved = await resolveImageResult(record.dataUrl);
        record.dataUrl = resolved.dataUrl;
        record.repairedAt = new Date().toISOString();
        delete record.repairError;
        await saveRecord(record);
        repaired += 1;
      } catch (error) {
        record.repairError = `远程图片转存失败：${error.message}`;
        await saveRecord(record);
        failed += 1;
      }
    }

    if (repaired > 0) {
      renderGalleryWithControls();
      updateStorageInfo();
      showStatus('done', `已修复 ${repaired} 条旧图片记录，图片已转存到浏览器`, 5000);
    }
    if (failed > 0) {
      appendEvent('event', `${failed} 条旧图片记录无法自动修复，远程链接可能已过期`);
    }
  }

  function readAllRecords(db) {
    return new Promise((resolve, reject) => {
      const tx = db.transaction(STORE_NAME, 'readonly');
      const request = tx.objectStore(STORE_NAME).getAll();
      request.onsuccess = () => resolve(request.result || []);
      request.onerror = () => reject(request.error);
    });
  }

  async function saveRecord(record) {
    const db = await openDB();
    return new Promise((resolve, reject) => {
      const tx = db.transaction(STORE_NAME, 'readwrite');
      tx.objectStore(STORE_NAME).put(record);
      tx.oncomplete = () => resolve();
      tx.onerror = () => reject(tx.error);
    });
  }

  async function deleteRecord(id) {
    const db = await openDB();
    return new Promise((resolve, reject) => {
      const tx = db.transaction(STORE_NAME, 'readwrite');
      tx.objectStore(STORE_NAME).delete(id);
      tx.oncomplete = () => resolve();
      tx.onerror = () => reject(tx.error);
    });
  }

  async function replaceGalleryRecords(records) {
    const db = await openDB();
    return new Promise((resolve, reject) => {
      const tx = db.transaction(STORE_NAME, 'readwrite');
      const store = tx.objectStore(STORE_NAME);
      store.clear();
      records.forEach((record) => store.put(record));
      tx.oncomplete = () => resolve();
      tx.onerror = () => reject(tx.error);
    });
  }

  async function addToGallery(dataUrl, prompt, mode, refDataUrl) {
    const now = new Date();
    const record = {
      id: Date.now() + Math.floor(Math.random() * 1000),
      dataUrl,
      prompt,
      mode,
      refDataUrl: refDataUrl || null,
      createdAt: now.toISOString(),
      time: now.toLocaleString(),
      params: getImageParams(),
    };
    await saveRecord(record);
    state.gallery.unshift(record);
    renderGalleryWithControls();
    updateStorageInfo();
    return record;
  }

  async function deleteFromGallery(id) {
    if (!confirm('确定删除这张图片记录吗？')) return;
    try {
      await deleteRecord(id);
      state.gallery = state.gallery.filter((item) => item.id !== id);
      renderGalleryWithControls();
      updateStorageInfo();
      showStatus('info', '图片记录已删除');
    } catch (error) {
      showStatus('err', `删除失败：${error.message}`);
    }
  }

  function renderGalleryWithControls() {
    if (!dom.galleryGrid) return;
    const filtered = filterByTags(sortGallery([...state.gallery], state.galleryView.sortMode));
    dom.galleryGrid.innerHTML = '';

    if (!filtered.length) {
      dom.galleryEmpty.hidden = false;
      dom.galleryCount.textContent = '';
      dom.topGalleryBadge.textContent = state.gallery.length ? `(${state.gallery.length})` : '';
      updateGalleryStats();
      return;
    }

    dom.galleryEmpty.hidden = true;
    dom.galleryCount.textContent = `(${filtered.length} 张)`;
    dom.topGalleryBadge.textContent = `(${state.gallery.length})`;

    if (state.galleryView.groupByMode || state.galleryView.groupByContent) {
      const groups = buildGalleryGroups(filtered);
      renderGroupedGallery(groups);
    } else {
      filtered.forEach((record) => dom.galleryGrid.appendChild(createGalleryCard(record)));
    }

    updateGalleryStats();
  }

  function sortGallery(items, mode) {
    if (mode === 'time-asc') {
      return items.sort((a, b) => new Date(a.createdAt || a.time || 0) - new Date(b.createdAt || b.time || 0));
    }
    if (mode === 'random') {
      for (let index = items.length - 1; index > 0; index--) {
        const randomIndex = Math.floor(Math.random() * (index + 1));
        [items[index], items[randomIndex]] = [items[randomIndex], items[index]];
      }
      return items;
    }
    return items.sort((a, b) => new Date(b.createdAt || b.time || 0) - new Date(a.createdAt || a.time || 0));
  }

  function filterByTags(items) {
    if (!state.galleryView.activeFilters.size) return items;
    return items.filter((item) => {
      const prompt = String(item.prompt || '').toLowerCase();
      return Array.from(state.galleryView.activeFilters).every((tag) => tagMatchesPrompt(tag, prompt));
    });
  }

  function tagMatchesPrompt(tag, prompt) {
    const rules = {
      woman: /woman|girl|female|lady|她|女性|女孩|女士/,
      man: /man|boy|male|gentleman|他|男性|男孩|男士/,
      anime: /anime|cartoon|manga|动漫|卡通/,
      realistic: /realistic|photo|photography|写实|照片/,
      landscape: /landscape|nature|mountain|sea|风景|自然/,
      asian: /asian|china|chinese|japanese|korean|亚洲|中国|日本|韩国/,
    };
    return rules[tag]?.test(prompt) || false;
  }

  function buildGalleryGroups(items) {
    const groups = new Map();
    items.forEach((item) => {
      const keys = [];
      if (state.galleryView.groupByMode) keys.push(item.mode === 2 ? '图生图' : '文生图');
      if (state.galleryView.groupByContent) keys.push(detectCategory(item.prompt));
      const key = keys.join(' / ') || '全部';
      if (!groups.has(key)) groups.set(key, []);
      groups.get(key).push(item);
    });
    return groups;
  }

  function renderGroupedGallery(groups) {
    groups.forEach((items, groupName) => {
      const block = document.createElement('section');
      block.className = 'group-block';
      const header = document.createElement('div');
      header.className = 'group-header';
      header.append(
        createTextElement('strong', '', groupName),
        createTextElement('span', '', `${items.length} 张`),
      );
      const grid = document.createElement('div');
      grid.className = 'group-grid';
      items.forEach((item) => grid.appendChild(createGalleryCard(item)));
      header.addEventListener('click', () => {
        grid.hidden = !grid.hidden;
      });
      block.append(header, grid);
      dom.galleryGrid.appendChild(block);
    });
  }

  function createGalleryCard(record) {
    const card = document.createElement('article');
    card.className = `gallery-item ${state.galleryView.displayMode === 'card' ? 'card-mode' : 'normal-mode'}`;

    const thumb = document.createElement('div');
    thumb.className = 'thumb-wrap';
    const imageIndex = state.gallery.findIndex((item) => item.id === record.id);

    if (state.galleryView.displayMode === 'card') {
      const inner = document.createElement('div');
      inner.className = 'flip-card-inner';
      const front = document.createElement('div');
      front.className = 'flip-card-front';
      front.appendChild(createTextElement('span', 'card-back-symbol', 'AI'));
      const back = document.createElement('div');
      back.className = 'flip-card-back';
      const image = createGalleryImage(record);
      back.appendChild(image);
      inner.append(front, back);
      thumb.appendChild(inner);
    } else {
      const image = createGalleryImage(record);
      image.classList.add('normal-image');
      thumb.appendChild(image);
    }

    thumb.addEventListener('click', () => openPreview(imageIndex >= 0 ? imageIndex : 0));
    thumb.appendChild(createTextElement('span', 'mode-badge', record.mode === 2 ? '图生图' : '文生图'));
    card.appendChild(thumb);

    const info = document.createElement('div');
    info.className = 'info';

    const tags = generatePromptTags(record.prompt);
    if (tags.length) {
      const tagsWrap = document.createElement('div');
      tagsWrap.className = 'prompt-tags';
      tags.forEach((tag) => tagsWrap.appendChild(createTextElement('span', 'prompt-tag', tag)));
      info.appendChild(tagsWrap);
    }

    if (record.refDataUrl) {
      const refRow = document.createElement('div');
      refRow.className = 'ref-row';
      const refThumb = document.createElement('button');
      refThumb.type = 'button';
      refThumb.className = 'ref-thumb';
      const refImg = document.createElement('img');
      refImg.src = record.refDataUrl;
      refImg.alt = '参考图';
      refThumb.appendChild(refImg);
      refThumb.addEventListener('click', (event) => {
        event.stopPropagation();
        openPreview(record.refDataUrl);
      });
      refRow.append(createTextElement('span', '', '参考图'), refThumb);
      info.appendChild(refRow);
    }

    const toggleBtn = createTextElement('button', 'prompt-toggle-btn', '查看提示词');
    toggleBtn.type = 'button';
    const promptRow = document.createElement('div');
    promptRow.className = 'prompt-row';
    promptRow.hidden = true;
    const promptEl = createTextElement('div', 'prompt-text', record.prompt || '');
    const copyBtn = createTextElement('button', 'copy-prompt', '复制');
    copyBtn.type = 'button';
    copyBtn.addEventListener('click', (event) => {
      event.stopPropagation();
      copyText(record.prompt, '提示词已复制');
    });
    promptRow.append(promptEl, copyBtn);
    toggleBtn.addEventListener('click', (event) => {
      event.stopPropagation();
      promptRow.hidden = !promptRow.hidden;
      toggleBtn.textContent = promptRow.hidden ? '查看提示词' : '收起提示词';
    });

    const meta = document.createElement('div');
    meta.className = 'meta';
    meta.appendChild(createTextElement('span', '', record.time || ''));
    const delBtn = createTextElement('button', 'del-btn', '删除');
    delBtn.type = 'button';
    delBtn.addEventListener('click', (event) => {
      event.stopPropagation();
      deleteFromGallery(record.id);
    });
    meta.appendChild(delBtn);

    info.append(toggleBtn, promptRow, meta);
    card.appendChild(info);
    return card;
  }

  function createGalleryImage(record) {
    const image = document.createElement('img');
    image.src = record.dataUrl;
    image.alt = record.prompt || '生成图片';
    image.loading = 'lazy';
    image.addEventListener('error', () => {
      image.replaceWith(createBrokenImageNotice(record));
    }, { once: true });
    return image;
  }

  function createBrokenImageNotice(record) {
    const notice = document.createElement('div');
    notice.className = 'broken-image-notice';
    const message = record.repairError || (isRemoteImageSource(record.dataUrl)
      ? '远程图片链接无法显示，可能已经过期'
      : '图片数据无法显示');
    notice.append(
      createTextElement('strong', '', '图片无法显示'),
      createTextElement('span', '', message),
    );
    return notice;
  }

  function detectCategory(prompt) {
    const text = String(prompt || '').toLowerCase();
    if (tagMatchesPrompt('woman', text)) return '女性人物';
    if (tagMatchesPrompt('man', text)) return '男性人物';
    if (tagMatchesPrompt('anime', text)) return '动漫风格';
    if (tagMatchesPrompt('realistic', text)) return '写实风格';
    if (tagMatchesPrompt('landscape', text)) return '风景场景';
    if (/city|urban|building|street|城市|建筑/.test(text)) return '城市场景';
    return '其他';
  }

  function generatePromptTags(prompt) {
    const text = String(prompt || '').toLowerCase();
    const tags = [];
    if (tagMatchesPrompt('woman', text)) tags.push('女性');
    if (tagMatchesPrompt('man', text)) tags.push('男性');
    if (tagMatchesPrompt('anime', text)) tags.push('动漫');
    if (tagMatchesPrompt('realistic', text)) tags.push('写实');
    if (tagMatchesPrompt('landscape', text)) tags.push('风景');
    if (/cat|猫/.test(text)) tags.push('猫');
    if (/dog|犬|狗/.test(text)) tags.push('狗');
    if (/cyberpunk|赛博朋克/.test(text)) tags.push('赛博朋克');
    return (tags.length ? tags : ['自定义']).slice(0, 6);
  }

  function updateGalleryStats() {
    const text2imgCount = state.gallery.filter((item) => item.mode !== 2).length;
    const img2imgCount = state.gallery.filter((item) => item.mode === 2).length;
    dom.text2imgCount.textContent = `${text2imgCount} 张`;
    dom.img2imgCount.textContent = `${img2imgCount} 张`;
    dom.totalCount.textContent = `${state.gallery.length} 张`;
  }

  function updateStorageInfo() {
    if (!dom.storageImageCount) return;
    dom.storageImageCount.textContent = `${state.gallery.length} 张`;
    dom.storageApiCount.textContent = `${state.apiConfigs.length} 个`;
    dom.storagePromptCount.textContent = `${state.promptHistory.length} 条`;
    const totalBytes = state.gallery.reduce((total, item) => {
      const imageSize = item.dataUrl ? item.dataUrl.length * 0.75 : 0;
      const refSize = item.refDataUrl ? item.refDataUrl.length * 0.75 : 0;
      return total + imageSize + refSize;
    }, 0);
    dom.storageSize.textContent = `${(totalBytes / (1024 * 1024)).toFixed(2)} MB`;
  }

  function resetCurrentRun() {
    if (dom.eventLog) {
      dom.eventLog.innerHTML = '';
      dom.eventLog.classList.remove('active');
    }
    if (dom.textStream) {
      dom.textStream.textContent = '';
      dom.textStream.classList.remove('active');
    }
    if (dom.resultGrid) dom.resultGrid.innerHTML = '';
    dom.resultArea?.classList.add('active');
    state.generation.done = 0;
    state.generation.success = 0;
    state.generation.failed = 0;
  }

  async function generate() {
    if (state.generation.active) {
      showStatus('info', '正在生成中，请稍候');
      return;
    }

    const validation = validateGenerationInputs();
    if (!validation.ok) {
      showStatus('err', validation.message);
      validation.focus?.focus();
      return;
    }

    const { baseUrl, apiKey, model, prompt } = validation;
    saveImageParams();
    saveApiConfigs();
    addPromptToHistory(prompt);

    if (state.refImages.length > 0) {
      await generateImageToImage(prompt, baseUrl, apiKey, model);
      return;
    }

    await generateBatch(prompt, baseUrl, apiKey, model, state.selectedGenCount);
  }

  function validateGenerationInputs() {
    const config = getActiveConfig();
    const prompt = dom.prompt.value.trim();

    if (!config) return { ok: false, message: '请先启用一个 API 配置', focus: dom.addNewApiConfig };
    if (!config.apiKey) return { ok: false, message: '请先在 API 配置中填写 API Key', focus: dom.addNewApiConfig };
    if (!config.model) return { ok: false, message: '请先在 API 配置中选择或填写 Model', focus: dom.addNewApiConfig };

    let baseUrl = '';
    try {
      baseUrl = normalizeBaseUrl(config.baseUrl);
    } catch (error) {
      return { ok: false, message: error.message, focus: dom.addNewApiConfig };
    }

    if (dom.baseUrl) dom.baseUrl.value = baseUrl;
    if (dom.apiKey) dom.apiKey.value = config.apiKey;
    if (dom.model) dom.model.value = config.model;

    if (!prompt) return { ok: false, message: '请填写提示词', focus: dom.prompt };
    return { ok: true, baseUrl, apiKey: config.apiKey, model: config.model, prompt };
  }

  async function generateBatch(prompt, baseUrl, apiKey, model, count) {
    beginGeneration(count);
    appendEvent('event', `开始批量生成 ${count} 张图片`);

    try {
      for (let index = 0; index < count; index += 1) {
        if (state.generation.cancelRequested) break;

        const enhancedPrompt = enhancePrompt(prompt, index);
        const label = index === 0 ? '原始提示词' : `增强版本 ${index}`;
        appendEvent('event', `生成第 ${index + 1}/${count} 张：${label}`);

        try {
          const result = await generateSingleImageWithRetry(enhancedPrompt, label, baseUrl, apiKey, model, null);
          state.generation.success += 1;
          try {
            await storeGeneratedImageResult(result, label, `img-${Date.now()}-${index}`, enhancedPrompt, 1, null);
          } catch (storeError) {
            appendEvent('event', storeError.message);
            showStatus('err', storeError.message, 9000);
          }
          appendEvent('done', `${label} 完成`);
        } catch (error) {
          state.generation.failed += 1;
          appendEvent('event', formatFetchError(error, `第 ${index + 1} 张`));
        } finally {
          state.generation.done += 1;
          updateProgress();
        }

        if (index < count - 1 && !state.generation.cancelRequested) {
          await sleep(GENERATION_DELAY_MS);
        }
      }

      const message = state.generation.cancelRequested
        ? `生成已取消。成功：${state.generation.success} 张，失败：${state.generation.failed} 张`
        : `批量生成完成。成功：${state.generation.success} 张，失败：${state.generation.failed} 张`;
      appendEvent('done', message);
      showStatus(state.generation.failed ? 'info' : 'done', message, 0);
    } finally {
      endGeneration();
    }
  }

  async function generateImageToImage(prompt, baseUrl, apiKey, model) {
    beginGeneration(1);
    appendEvent('event', '检测到参考图片，进入图生图模式');

    try {
      const refDataUrl = state.refImages[0].dataUrl;
      const result = await generateSingleImageWithRetry(prompt, '编辑结果', baseUrl, apiKey, model, refDataUrl);
      try {
        await storeGeneratedImageResult(result, '编辑结果', `img-${Date.now()}-single`, prompt, 2, refDataUrl);
      } catch (storeError) {
        appendEvent('event', storeError.message);
        showStatus('err', storeError.message, 9000);
      }
      clearRefImages();
      state.generation.success = 1;
      state.generation.done = 1;
      updateProgress();
      appendEvent('done', '图片生成完成');
      showStatus('done', '图片生成完成', 0);
    } catch (error) {
      state.generation.failed = 1;
      state.generation.done = 1;
      updateProgress();
      appendEvent('event', formatFetchError(error, '图生图'));
      showStatus('err', formatFetchError(error, '图生图'), 0);
    } finally {
      endGeneration();
    }
  }

  function beginGeneration(total) {
    state.generation.active = true;
    state.generation.cancelRequested = false;
    state.generation.total = total;
    state.generation.done = 0;
    state.generation.success = 0;
    state.generation.failed = 0;
    resetCurrentRun();
    lockInputs(true);
    setGenerateButtonState(true, '生成中...');
    showProgress(total);
    startTimer();
    dom.loadingMini?.classList.add('active');
  }

  function endGeneration() {
    state.generation.active = false;
    state.generation.cancelRequested = false;
    if (state.generation.abortController) {
      state.generation.abortController = null;
    }
    stopTimer();
    lockInputs(false);
    setGenerateButtonState(false, '开始生成');
    hideProgress();
    dom.loadingMini?.classList.remove('active');
  }

  function cancelGeneration() {
    if (!state.generation.active) return;
    state.generation.cancelRequested = true;
    state.generation.abortController?.abort();
    appendEvent('event', '用户请求取消生成');
    if (dom.progressText) dom.progressText.textContent = '正在取消...';
  }

  function setGenerateButtonState(disabled, text) {
    if (!dom.genBtn) return;
    dom.genBtn.disabled = disabled;
    const label = $('.gen-btn-text', dom.genBtn);
    if (label) label.textContent = text;
  }

  function showProgress(total) {
    if (!dom.genProgress) return;
    dom.genProgress.hidden = false;
    state.generation.total = total;
    updateProgress();
  }

  function hideProgress() {
    if (dom.genProgress) dom.genProgress.hidden = true;
  }

  function updateProgress() {
    const total = Math.max(1, state.generation.total);
    const percent = Math.min(100, (state.generation.done / total) * 100);
    dom.progressBar.style.width = `${percent}%`;
    dom.progressText.textContent = `正在生成 ${state.generation.done}/${state.generation.total}...`;
    dom.progressSuccess.textContent = String(state.generation.success);
    dom.progressFailed.textContent = String(state.generation.failed);
    dom.progressRemaining.textContent = String(Math.max(0, state.generation.total - state.generation.done));
  }

  function startTimer() {
    state.generation.startTime = Date.now();
    window.clearInterval(state.generation.timer);
    state.generation.timer = window.setInterval(() => {
      const seconds = Math.floor((Date.now() - state.generation.startTime) / 1000);
      dom.statElapsed.textContent = `${seconds}s`;
    }, 1000);
  }

  function stopTimer() {
    window.clearInterval(state.generation.timer);
    state.generation.timer = null;
  }

  async function generateSingleImageWithRetry(prompt, label, baseUrl, apiKey, model, refDataUrl, maxAttempts = 3) {
    let lastError = null;

    for (let attempt = 1; attempt <= maxAttempts; attempt += 1) {
      if (state.generation.cancelRequested) throw new Error('生成已取消');
      try {
        if (attempt > 1) appendEvent('event', `${label} 第 ${attempt} 次重试，模型：${model}`);
        return await generateSingleImage(prompt, label, baseUrl, apiKey, model, refDataUrl);
      } catch (error) {
        lastError = error;
        if (!isRetryableImageError(error) || attempt === maxAttempts) break;
        await sleep(RETRY_DELAY_MS);
      }
    }

    throw lastError || new Error('生成失败');
  }

  async function generateSingleImage(promptText, label, baseUrl, apiKey, model, refDataUrl) {
    const params = getImageParams();
    if (usesImagesGenerationsEndpoint(model, refDataUrl)) {
      return generateSingleImageViaImagesEndpoint(promptText, label, baseUrl, apiKey, model, params);
    }

    const payload = buildImagePayload(promptText, model, params, refDataUrl);
    const controller = new AbortController();
    state.generation.abortController = controller;
    const timeoutId = window.setTimeout(() => controller.abort(), 20 * 60 * 1000);

    try {
      const request = createApiRequest(baseUrl, '/v1/responses', {
        Authorization: `Bearer ${apiKey}`,
        Accept: 'text/event-stream, application/json',
        'Content-Type': 'application/json',
      });
      const response = await fetch(request.url, {
        method: 'POST',
        headers: request.headers,
        body: JSON.stringify(payload),
        signal: controller.signal,
        mode: 'cors',
        credentials: 'omit',
        cache: 'no-cache',
      });

      if (!response.ok) {
        let body = '';
        try {
          body = await response.text();
        } catch {}
        throw new Error(`HTTP ${response.status}${body ? ` - ${body.slice(0, 500)}` : ''}`);
      }

      const imageSource = await readImageResponse(response);
      return { imageSource, mediaAuth: buildMediaAuthContext(baseUrl, apiKey) };
    } catch (error) {
      if (error.name === 'AbortError') {
        throw new Error(state.generation.cancelRequested ? '生成已取消' : '请求超时');
      }
      throw error;
    } finally {
      window.clearTimeout(timeoutId);
    }
  }

  function usesImagesGenerationsEndpoint(model, refDataUrl) {
    return !refDataUrl && /^gpt-image-/i.test(String(model || ''));
  }

  async function generateSingleImageViaImagesEndpoint(promptText, label, baseUrl, apiKey, model, params) {
    const controller = new AbortController();
    state.generation.abortController = controller;
    const timeoutId = window.setTimeout(() => controller.abort(), 20 * 60 * 1000);
    const payload = {
      model,
      prompt: promptText,
      n: 1,
      size: params.size,
      quality: normalizeImagesEndpointQuality(params.quality),
      output_format: 'png',
    };

    try {
      const request = createApiRequest(baseUrl, '/v1/images/generations', {
        Authorization: `Bearer ${apiKey}`,
        Accept: 'application/json',
        'Content-Type': 'application/json',
      });
      const response = await fetch(request.url, {
        method: 'POST',
        headers: request.headers,
        body: JSON.stringify(payload),
        signal: controller.signal,
        mode: 'cors',
        credentials: 'omit',
        cache: 'no-cache',
      });

      if (!response.ok) {
        let body = '';
        try {
          body = await response.text();
        } catch {}
        throw new Error(`HTTP ${response.status}${body ? ` - ${body.slice(0, 500)}` : ''}`);
      }

      const data = await response.json();
      const imageSource = extractImageDataUrl(data);
      if (!imageSource) throw new Error('No image data returned from images endpoint');
      appendEvent('event', `${label} completed via images/generations`);
      return { imageSource, mediaAuth: buildMediaAuthContext(baseUrl, apiKey) };
    } catch (error) {
      if (error.name === 'AbortError') {
        throw new Error(state.generation.cancelRequested ? 'Generation cancelled' : 'Request timed out');
      }
      throw error;
    } finally {
      window.clearTimeout(timeoutId);
    }
  }

  function normalizeImagesEndpointQuality(quality) {
    if (quality === 'hd') return 'high';
    if (quality === 'standard') return 'medium';
    if (['low', 'medium', 'high', 'auto'].includes(quality)) return quality;
    return 'medium';
  }

  function buildImagePayload(promptText, model, params, refDataUrl) {
    const tool = {
      type: 'image_generation',
      output_format: 'png',
      size: params.size,
      quality: params.quality,
      style: params.style,
    };

    if (refDataUrl) {
      return {
        model,
        input: [
          {
            role: 'user',
            content: [
              { type: 'input_image', image_url: refDataUrl },
              { type: 'input_text', text: `请根据以下要求，对我提供的参考图片进行编辑修改，直接生成修改后的新图片。要求：${promptText}` },
            ],
          },
        ],
        tools: [tool],
        tool_choice: { type: 'image_generation' },
        stream: true,
      };
    }

    return {
      model,
      input: [
        {
          role: 'system',
          content: '你是一个图片生成助手。用户要求你生成图片时，你必须调用 image_generation 工具直接生成图片，不要只用文字描述。',
        },
        {
          role: 'user',
          content: `请生成以下描述的图片：${promptText}`,
        },
      ],
      tools: [tool],
      tool_choice: { type: 'image_generation' },
      stream: true,
    };
  }

  async function readImageResponse(response) {
    const contentType = response.headers.get('content-type') || '';
    if (contentType.includes('application/json') || !response.body?.getReader) {
      const text = await response.text();
      const data = safeJsonParse(text);
      const dataUrl = extractImageDataUrl(data ?? text);
      if (dataUrl) return dataUrl;
      if (hasTextOnlyResponse(data ?? text)) {
        throw new Error('模型未调用 image_generation 工具，只返回了文字。请确认当前选择的 Model 支持图片生成工具。');
      }
      throw new Error('响应中没有图片数据');
    }

    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let buffer = '';
    const diagnostics = { textChars: 0 };

    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      buffer += decoder.decode(value, { stream: true });
      const lines = buffer.split('\n');
      buffer = lines.pop() || '';

      for (const rawLine of lines) {
        const dataUrl = processImageStreamLine(rawLine, diagnostics);
        if (dataUrl) return dataUrl;
      }
    }

    const remainingImage = processImageStreamLine(buffer, diagnostics);
    if (remainingImage) return remainingImage;
    if (diagnostics.textChars > 0) {
      throw new Error('模型未调用 image_generation 工具，只返回了文字。请确认当前选择的 Model 支持图片生成工具。');
    }
    throw new Error('流结束但没有返回图片');
  }

  function processImageStreamLine(rawLine, diagnostics = null) {
    const line = String(rawLine || '').trim();
    if (!line) return null;

    if (line.startsWith('event:')) {
      appendEvent('event', line.slice(6).trim());
      return null;
    }

    const dataText = line.startsWith('data:')
      ? line.slice(5).trim()
      : line;
    if (!dataText || dataText === '[DONE]') return null;

    const data = safeJsonParse(dataText);
    if (data?.type === 'error' || data?.error) {
      throw new Error(data.error?.message || String(data.error));
    }

    const dataUrl = extractImageDataUrl(data ?? dataText);
    if (dataUrl) return dataUrl;

    const text = extractTextChunk(data);
    if (text) {
      if (diagnostics) diagnostics.textChars += text.length;
      appendTextChunk(text);
    }
    return null;
  }

  function extractImageDataUrl(input) {
    const seen = new Set();
    const preferredKeys = ['dataUrl', 'image_url', 'url', 'b64_json', 'image_base64', 'base64', 'result', 'output', 'content'];

    function walk(value) {
      if (value == null) return null;
      if (typeof value === 'string') {
        const text = value.trim();
        if (/^data:image\/[a-zA-Z0-9.+-]+;base64,/.test(text)) return text;
        if (/^https?:\/\//i.test(text)) return text;
        if (looksLikeBase64Image(text)) return `data:image/png;base64,${text}`;
        return null;
      }
      if (typeof value !== 'object') return null;
      if (seen.has(value)) return null;
      seen.add(value);

      if (Array.isArray(value)) {
        for (const item of value) {
          const found = walk(item);
          if (found) return found;
        }
        return null;
      }

      for (const key of preferredKeys) {
        if (key in value) {
          const found = walk(value[key]);
          if (found) return found;
        }
      }

      for (const item of Object.values(value)) {
        const found = walk(item);
        if (found) return found;
      }

      return null;
    }

    return walk(input);
  }

  function looksLikeBase64Image(text) {
    if (text.length < 400) return false;
    if (!/^[A-Za-z0-9+/=\s]+$/.test(text)) return false;
    return text.length % 4 === 0 || text.endsWith('=');
  }

  function extractTextChunk(data) {
    if (!data || typeof data !== 'object') return '';
    const candidates = [
      data.delta,
      data.text,
      data.output_text,
      data.message?.content,
      data.response?.output_text,
    ];
    return candidates.find((value) => typeof value === 'string' && value.trim()) || '';
  }

  function hasTextOnlyResponse(data) {
    if (typeof data === 'string') return data.trim().length > 0;
    if (!data || typeof data !== 'object') return false;
    return Boolean(extractTextChunk(data));
  }

  function safeJsonParse(text) {
    if (typeof text !== 'string') return text;
    try {
      return JSON.parse(text);
    } catch {
      return null;
    }
  }

  function enhancePrompt(basePrompt, index) {
    const enhancements = [
      '',
      'highly detailed, intricate details, sharp focus, 8k resolution',
      'dramatic lighting, cinematic lighting, volumetric lighting, studio lighting',
      'masterpiece, best quality, professional, award winning',
      'perfect composition, rule of thirds, depth of field, dynamic angle',
      'vibrant colors, rich colors, color grading, color harmony',
      'photorealistic, ultra detailed, lifelike textures',
      'atmospheric, moody, ethereal, dreamy',
      'professional photography, magazine cover, studio quality',
      'dynamic pose, interesting perspective, creative composition',
      'soft focus, bokeh, shallow depth of field',
      'golden hour lighting, natural lighting',
      'visually stunning, eye-catching, impressive',
      'high resolution, ultra hd, crystal clear',
      'artistic, creative, unique style',
    ];
    const suffix = enhancements[index % enhancements.length];
    return suffix ? `${basePrompt}, ${suffix}` : basePrompt;
  }

  function isRetryableImageError(error) {
    const message = String(error?.message || error || '');
    if (/未调用 image_generation|只返回了文字|响应中没有图片数据|流结束但没有返回图片/i.test(message)) {
      return false;
    }
    return /HTTP\s*5\d\d|负载|上限|繁忙|稍后重试|get_channel|rate limit|timeout|超时|网络连接失败/i.test(message);
  }

  function formatFetchError(error, context = '') {
    let message = String(error?.message || error || '未知错误');
    const suggestions = [];

    if (/file:\/\/|同源代理|本地代理服务|Cloudflare Pages/i.test(message)) {
      message = '当前打开方式无法代理 API';
      suggestions.push('使用本地代理服务地址打开页面', '或部署到 Cloudflare Pages 后访问站点地址', '不要用 file:// 直接打开 index.html 调用这个接口');
    } else if (/Failed to fetch|NetworkError|网络/i.test(message)) {
      message = '网络连接失败';
      suggestions.push('检查 Base URL 是否正确', '确认 API 服务允许浏览器 CORS 访问', '确认网络或代理可用');
    } else if (/CORS/i.test(message)) {
      message = 'CORS 跨域请求被阻止';
      suggestions.push('需要服务端允许浏览器来源', '可换支持 CORS 的 API 服务');
    } else if (/HTTP\s*401|unauthorized|invalid api key/i.test(message)) {
      message = 'API Key 无效或无权限';
      suggestions.push('检查 API Key', '确认模型权限');
    } else if (/HTTP\s*429|rate limit/i.test(message)) {
      message = '请求过于频繁或额度受限';
      suggestions.push('降低生成数量', '稍后重试');
    } else if (/HTTP\s*5\d\d|负载|上限|get_channel/i.test(message)) {
      message = '模型或通道负载过高';
      suggestions.push('稍后重试', '换一个模型', '先生成 1 张确认链路');
    } else if (/未调用 image_generation|只返回了文字|响应中没有图片数据|流结束但没有返回图片/i.test(message)) {
      message = '当前模型没有返回图片';
      suggestions.push('确认当前 Model 支持 image_generation 工具', '不要选择只会文本输出的模型', '先用 1 张图测试模型链路');
    }

    let fullMessage = context ? `${context}: ${message}` : message;
    if (suggestions.length) {
      fullMessage += `\n建议：\n- ${suggestions.join('\n- ')}`;
    }
    return fullMessage;
  }

  async function storeGeneratedImageResult(result, label, imageName, prompt, mode, refDataUrl) {
    try {
      const resolved = await resolveImageResult(result.imageSource, result.mediaAuth);
      await addResultCard(label, imageName, prompt, resolved.dataUrl, resolved.blob);
      await addToGallery(resolved.dataUrl, prompt, mode, refDataUrl);
      return resolved;
    } catch (error) {
      throw new Error(`${label} 图片已生成，但没能转存到浏览器本地图库：${error.message}`);
    }
  }

  async function addResultCard(label, imageName, prompt, dataUrl, blob) {
    const card = document.createElement('article');
    card.className = 'result-card';
    card.appendChild(createTextElement('div', 'label', label));

    const image = document.createElement('img');
    image.src = dataUrl;
    image.alt = prompt || imageName;
    image.addEventListener('click', () => openPreview(dataUrl));
    card.appendChild(image);

    const actions = document.createElement('div');
    actions.className = 'actions';
    const downloadBtn = createTextElement('button', '', '下载');
    downloadBtn.type = 'button';
    downloadBtn.addEventListener('click', () => downloadImage(dataUrl, `${imageName}.png`));

    const copyBtn = createTextElement('button', 'secondary', '复制');
    copyBtn.type = 'button';
    copyBtn.disabled = !blob;
    if (!blob) copyBtn.title = '远程图片未能转成本地 Blob，暂不能复制图片';
    copyBtn.addEventListener('click', async () => {
      try {
        if (!blob) throw new Error('当前图片暂不能复制，请先打开或下载图片');
        if (!navigator.clipboard?.write || typeof ClipboardItem === 'undefined') {
          throw new Error('当前浏览器不支持复制图片');
        }
        await navigator.clipboard.write([new ClipboardItem({ 'image/png': blob })]);
        showStatus('done', '图片已复制');
      } catch (error) {
        showStatus('err', `复制失败：${error.message}`);
      }
    });

    actions.append(downloadBtn, copyBtn);
    card.appendChild(actions);
    dom.resultGrid.appendChild(card);
  }

  function appendEvent(type, message) {
    if (!dom.eventLog) return;
    dom.eventLog.classList.add('active');
    const line = document.createElement('div');
    line.className = 'event-line';
    const labelMap = {
      event: 'EVENT',
      data: 'DATA',
      text: 'TEXT',
      done: 'DONE',
    };
    const tagClass = type === 'done' ? 'done-tag' : type === 'data' ? 'data-tag' : type === 'text' ? 'text-tag' : 'event-tag';
    line.append(
      createTextElement('span', tagClass, labelMap[type] || 'EVENT'),
      createTextElement('span', '', `[${new Date().toTimeString().slice(0, 8)}] ${message}`),
    );
    dom.eventLog.appendChild(line);
    dom.eventLog.scrollTop = dom.eventLog.scrollHeight;
    dom.statEvents.textContent = `事件: ${dom.eventLog.children.length}`;
  }

  function appendTextChunk(text) {
    if (!dom.textStream) return;
    dom.textStream.classList.add('active');
    dom.textStream.textContent += text;
    dom.statTextLen.textContent = `文本: ${dom.textStream.textContent.length} 字`;
  }

  function downloadImage(dataUrl, filename) {
    const link = document.createElement('a');
    link.href = dataUrl;
    link.download = filename;
    document.body.appendChild(link);
    link.click();
    link.remove();
  }

  function dataUrlToBlob(dataUrl) {
    return fetch(dataUrl).then((response) => response.blob());
  }

  function isRemoteImageSource(value) {
    return /^https?:\/\//i.test(String(value || '').trim());
  }

  async function resolveImageResult(imageSource, mediaAuth = null) {
    const blob = await imageSourceToBlob(imageSource, mediaAuth);
    return {
      dataUrl: await blobToDataUrl(blob),
      blob,
    };
  }

  async function imageSourceToBlob(imageSource, mediaAuth = null) {
    const source = String(imageSource || '').trim();
    if (/^data:image\//i.test(source)) {
      return dataUrlToBlob(source);
    }
    if (/^https?:\/\//i.test(source)) {
      const response = await fetchProxiedMedia(source, mediaAuth);
      if (!response.ok) {
        throw new Error(await formatMediaDownloadError(response, source));
      }
      return await response.blob();
    }
    throw new Error('Unsupported image source returned by API');
  }

  async function fetchProxiedMedia(source, mediaAuth) {
    if (window.location.protocol === 'http:' || window.location.protocol === 'https:') {
      const headers = {
        Accept: 'image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8',
        'Content-Type': 'application/json',
      };
      const authHeader = mediaAuthHeaderForSource(source, mediaAuth);
      if (authHeader) headers.Authorization = authHeader;
      return fetch(`${window.location.origin}/__picture_media`, {
        method: 'POST',
        headers,
        body: JSON.stringify({ url: source }),
        cache: 'no-cache',
      });
    }
    return fetch(source, { method: 'GET', cache: 'no-cache' });
  }

  async function formatMediaDownloadError(response, source) {
    let body = '';
    try {
      body = await response.text();
    } catch {}
    const detail = body ? ` - ${body.slice(0, 500)}` : '';
    return `Image download failed: HTTP ${response.status}${detail} (${summarizeMediaSource(source)})`;
  }

  function buildMediaAuthContext(baseUrl, apiKey) {
    try {
      return {
        origin: new URL(normalizeBaseUrl(baseUrl)).origin,
        authorization: `Bearer ${apiKey}`,
      };
    } catch {
      return null;
    }
  }

  function mediaAuthHeaderForSource(source, mediaAuth) {
    if (!mediaAuth?.origin || !mediaAuth.authorization) return '';
    try {
      return new URL(source).origin === mediaAuth.origin ? mediaAuth.authorization : '';
    } catch {
      return '';
    }
  }

  function summarizeMediaSource(source) {
    try {
      const url = new URL(source);
      const path = url.pathname.length > 90 ? `${url.pathname.slice(0, 90)}...` : url.pathname;
      return `${url.origin}${path}`;
    } catch {
      return 'invalid media source';
    }
  }

  function blobToDataUrl(blob) {
    return new Promise((resolve, reject) => {
      const reader = new FileReader();
      reader.onload = () => resolve(String(reader.result || ''));
      reader.onerror = () => reject(reader.error || new Error('Failed to read image blob'));
      reader.readAsDataURL(blob);
    });
  }

  function sleep(ms) {
    return new Promise((resolve) => window.setTimeout(resolve, ms));
  }

  function fetchWithTimeout(url, options = {}, timeoutMs = 10000) {
    const controller = new AbortController();
    const timeoutId = window.setTimeout(() => controller.abort(), timeoutMs);
    return fetch(url, { ...options, signal: controller.signal }).finally(() => {
      window.clearTimeout(timeoutId);
    });
  }

  function fetchApiWithTimeout(baseUrl, path, options = {}, timeoutMs = 10000) {
    const request = createApiRequest(baseUrl, path, options.headers || {});
    return fetchWithTimeout(request.url, { ...options, headers: request.headers }, timeoutMs);
  }

  async function quickNetworkTest() {
    const validation = validateNetworkInputs();
    if (!validation.ok) {
      showDiagResult(validation.message, 'err');
      return;
    }

    state.network.checking = true;
    updateNetworkStatusDisplay();
    const startedAt = Date.now();

    try {
      const response = await fetchApiWithTimeout(validation.baseUrl, '/v1/models', {
        headers: {
          Authorization: `Bearer ${validation.apiKey}`,
        },
      }, 12000);
      state.network.latency = Date.now() - startedAt;
      state.network.isOnline = response.ok;
      state.network.lastCheck = new Date().toLocaleTimeString();
      if (!response.ok) throw new Error(`HTTP ${response.status}: ${response.statusText}`);
      showDiagResult(`模型接口可访问，延迟 ${state.network.latency}ms`, 'done');
    } catch (error) {
      state.network.isOnline = false;
      state.network.lastCheck = new Date().toLocaleTimeString();
      showDiagResult(formatFetchError(error, '快速检测'), 'err');
    } finally {
      state.network.checking = false;
      updateNetworkStatusDisplay();
    }
  }

  async function fullNetworkDiagnosis() {
    const lines = [];
    lines.push(`浏览器在线状态：${navigator.onLine ? '在线' : '离线'}`);

    const validation = validateNetworkInputs();
    if (!validation.ok) {
      lines.push(validation.message);
      showDiagResult(lines.join('\n'), 'err');
      return;
    }

    lines.push(`Base URL：${validation.baseUrl}`);
    lines.push(`Model：${dom.model.value.trim() || '-'}`);

    const startedAt = Date.now();
    try {
      const models = await fetchModelIds(validation.baseUrl, validation.apiKey);
      lines.push(`GET /v1/models：成功，${models.length} 个模型，${Date.now() - startedAt}ms`);
      showDiagResult(lines.join('\n'), 'done');
    } catch (error) {
      lines.push(`GET /v1/models：失败`);
      lines.push(formatFetchError(error));
      showDiagResult(lines.join('\n'), 'err');
    }
  }

  function validateNetworkInputs() {
    let baseUrl = '';
    try {
      baseUrl = normalizeBaseUrl(dom.baseUrl.value);
    } catch (error) {
      return { ok: false, message: error.message };
    }
    const apiKey = dom.apiKey.value.trim();
    if (!apiKey) return { ok: false, message: '请先选择或填写 API Key' };
    return { ok: true, baseUrl, apiKey };
  }

  function showDiagResult(text, type = 'info') {
    if (!dom.diagResult) return;
    dom.diagResult.textContent = text;
    dom.diagResult.className = `diag-result show ${type}`;
  }

  function updateNetworkStatusDisplay() {
    if (state.network.checking) {
      dom.networkStatusText.textContent = '检测中...';
      dom.networkStatusDot.className = 'status-dot checking';
      dom.connectionStatus.textContent = '检测中';
      dom.networkStatusValue.textContent = '检测中';
      return;
    }

    const online = state.network.isOnline;
    dom.networkStatusText.textContent = online ? '在线' : '离线';
    dom.networkStatusDot.className = `status-dot ${online ? 'online' : 'offline'}`;
    dom.connectionStatus.textContent = online ? '正常' : '断开';
    dom.connectionStatus.style.color = online ? 'var(--success)' : 'var(--danger)';
    dom.networkStatusValue.textContent = online ? '在线' : '离线';

    if (state.network.latency != null) {
      dom.networkLatency.textContent = `${state.network.latency}ms`;
      dom.networkPing.hidden = false;
      dom.networkPing.textContent = `${state.network.latency}ms`;
    } else {
      dom.networkLatency.textContent = '-';
      dom.networkPing.hidden = true;
    }
    dom.lastCheckTime.textContent = state.network.lastCheck || '-';
  }

  function exportAllData() {
    const exportData = {
      version: '1.0',
      exportDate: new Date().toISOString(),
      gallery: state.gallery,
      apiConfigs: state.apiConfigs,
      activeApiId: state.activeApiId,
      promptHistory: state.promptHistory,
      imageParams: state.imageParams,
      autoDownload: state.autoDownload,
    };
    const blob = new Blob([JSON.stringify(exportData, null, 2)], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = `ai-image-gen-backup-${new Date().toISOString().slice(0, 10)}.json`;
    document.body.appendChild(link);
    link.click();
    link.remove();
    URL.revokeObjectURL(url);
    showStatus('done', '数据导出成功');
  }

  async function importAllData(event) {
    const file = event.target.files?.[0];
    if (!file) return;

    try {
      const data = JSON.parse(await file.text());
      if (!data || !Array.isArray(data.gallery)) {
        throw new Error('数据格式不正确');
      }
      const message = `确定导入数据吗？\n\n图片：${data.gallery.length} 张\nAPI 配置：${data.apiConfigs?.length || 0} 个\n提示词历史：${data.promptHistory?.length || 0} 条\n\n当前数据会被覆盖。`;
      if (!confirm(message)) return;

      state.gallery = data.gallery;
      state.apiConfigs = Array.isArray(data.apiConfigs) && data.apiConfigs.length
        ? data.apiConfigs
        : structuredCloneSafe(DEFAULT_CONFIGS);
      const importedActiveId = data.activeApiId || state.apiConfigs[0]?.id || null;
      state.activeApiId = state.apiConfigs.some((config) => config.id === importedActiveId)
        ? importedActiveId
        : state.apiConfigs[0]?.id || null;
      state.promptHistory = Array.isArray(data.promptHistory) ? data.promptHistory.slice(0, MAX_PROMPT_HISTORY) : [];
      state.imageParams = { ...state.imageParams, ...(data.imageParams || {}) };
      state.autoDownload = Boolean(data.autoDownload);

      await replaceGalleryRecords(state.gallery);
      saveApiConfigs();
      savePromptHistory();
      saveImageParams();
      saveAutoDownloadSetting();
      loadImageParams();
      renderAll();
      dom.autoDownloadCheckbox.checked = state.autoDownload;
      showStatus('done', `数据导入成功，已导入 ${state.gallery.length} 张图片`);
    } catch (error) {
      showStatus('err', `导入失败：${error.message}`);
    } finally {
      dom.importFileInput.value = '';
    }
  }

  async function downloadAllImages() {
    if (!state.gallery.length) {
      showStatus('err', '没有可下载的图片');
      return;
    }

    try {
      const files = [];
      state.gallery.forEach((item, index) => {
        const base64 = String(item.dataUrl || '').split(',')[1];
        if (!base64) return;
        const fileName = sanitizeFileName(`image-${item.id || index + 1}.png`);
        files.push({
          name: `ai-generated-images/${fileName}`,
          bytes: base64ToBytes(base64),
        });
        files.push({
          name: `ai-generated-images/${fileName}.json`,
          bytes: textToUtf8Bytes(JSON.stringify({
            id: item.id,
            prompt: item.prompt,
            mode: item.mode,
            time: item.time,
            params: item.params,
          }, null, 2)),
        });
      });

      if (!files.length) {
        showStatus('err', '没有有效图片数据可下载');
        return;
      }

      const content = createZipBlob(files);
      const url = URL.createObjectURL(content);
      const link = document.createElement('a');
      link.href = url;
      link.download = `ai-images-${new Date().toISOString().slice(0, 10)}.zip`;
      document.body.appendChild(link);
      link.click();
      link.remove();
      URL.revokeObjectURL(url);
      showStatus('done', `成功打包 ${state.gallery.length} 张图片`);
    } catch (error) {
      showStatus('err', `批量下载失败：${error.message}`);
    }
  }

  function createZipBlob(files) {
    const localParts = [];
    const centralParts = [];
    let offset = 0;

    files.forEach((file) => {
      const nameBytes = textToUtf8Bytes(file.name);
      const data = file.bytes;
      const crc = crc32(data);
      const { time, date } = getDosDateTime(new Date());

      const localHeader = new Uint8Array(30 + nameBytes.length);
      const localView = new DataView(localHeader.buffer);
      localView.setUint32(0, 0x04034b50, true);
      localView.setUint16(4, 20, true);
      localView.setUint16(6, 0x0800, true);
      localView.setUint16(8, 0, true);
      localView.setUint16(10, time, true);
      localView.setUint16(12, date, true);
      localView.setUint32(14, crc, true);
      localView.setUint32(18, data.length, true);
      localView.setUint32(22, data.length, true);
      localView.setUint16(26, nameBytes.length, true);
      localHeader.set(nameBytes, 30);
      localParts.push(localHeader, data);

      const centralHeader = new Uint8Array(46 + nameBytes.length);
      const centralView = new DataView(centralHeader.buffer);
      centralView.setUint32(0, 0x02014b50, true);
      centralView.setUint16(4, 20, true);
      centralView.setUint16(6, 20, true);
      centralView.setUint16(8, 0x0800, true);
      centralView.setUint16(10, 0, true);
      centralView.setUint16(12, time, true);
      centralView.setUint16(14, date, true);
      centralView.setUint32(16, crc, true);
      centralView.setUint32(20, data.length, true);
      centralView.setUint32(24, data.length, true);
      centralView.setUint16(28, nameBytes.length, true);
      centralView.setUint32(42, offset, true);
      centralHeader.set(nameBytes, 46);
      centralParts.push(centralHeader);

      offset += localHeader.length + data.length;
    });

    const centralSize = centralParts.reduce((total, part) => total + part.length, 0);
    const endHeader = new Uint8Array(22);
    const endView = new DataView(endHeader.buffer);
    endView.setUint32(0, 0x06054b50, true);
    endView.setUint16(8, files.length, true);
    endView.setUint16(10, files.length, true);
    endView.setUint32(12, centralSize, true);
    endView.setUint32(16, offset, true);

    return new Blob([...localParts, ...centralParts, endHeader], { type: 'application/zip' });
  }

  function textToUtf8Bytes(text) {
    return new TextEncoder().encode(String(text));
  }

  function base64ToBytes(base64) {
    const binary = atob(String(base64).replace(/\s/g, ''));
    const bytes = new Uint8Array(binary.length);
    for (let index = 0; index < binary.length; index += 1) {
      bytes[index] = binary.charCodeAt(index);
    }
    return bytes;
  }

  function crc32(bytes) {
    let crc = 0xffffffff;
    for (let index = 0; index < bytes.length; index += 1) {
      crc = CRC32_TABLE[(crc ^ bytes[index]) & 0xff] ^ (crc >>> 8);
    }
    return (crc ^ 0xffffffff) >>> 0;
  }

  const CRC32_TABLE = (() => {
    const table = new Uint32Array(256);
    for (let index = 0; index < 256; index += 1) {
      let value = index;
      for (let bit = 0; bit < 8; bit += 1) {
        value = value & 1 ? 0xedb88320 ^ (value >>> 1) : value >>> 1;
      }
      table[index] = value >>> 0;
    }
    return table;
  })();

  function getDosDateTime(date) {
    const year = Math.max(1980, date.getFullYear());
    const time = (date.getHours() << 11) | (date.getMinutes() << 5) | Math.floor(date.getSeconds() / 2);
    const dosDate = ((year - 1980) << 9) | ((date.getMonth() + 1) << 5) | date.getDate();
    return { time, date: dosDate };
  }

  function sanitizeFileName(name) {
    return String(name || 'image.png')
      .replace(/[<>:"/\\|?*\x00-\x1f]/g, '_')
      .slice(0, 120) || 'image.png';
  }

  async function clearAllData() {
    if (!confirm('确定要清空所有数据吗？这会删除图片、API 配置、提示词历史和设置。')) return;
    if (!confirm('最后确认：建议先导出备份。仍然清空吗？')) return;

    try {
      state.gallery = [];
      await replaceGalleryRecords([]);
      Object.values(STORAGE_KEYS).forEach((key) => localStorage.removeItem(key));
      state.apiConfigs = structuredCloneSafe(DEFAULT_CONFIGS);
      state.activeApiId = state.apiConfigs[0].id;
      state.promptHistory = [];
      state.refImages = [];
      state.autoDownload = false;
      state.imageParams = { size: '1024x1024', quality: 'standard', style: 'natural' };
      saveApiConfigs();
      savePromptHistory();
      saveImageParams();
      saveAutoDownloadSetting();
      loadImageParams();
      dom.autoDownloadCheckbox.checked = false;
      renderAll();
      showStatus('done', '所有数据已清空');
    } catch (error) {
      showStatus('err', `清空失败：${error.message}`);
    }
  }

  function loadAutoDownloadSetting() {
    state.autoDownload = false;
    localStorage.removeItem(STORAGE_KEYS.autoDownload);
    if (dom.autoDownloadCheckbox) dom.autoDownloadCheckbox.checked = true;
  }

  function saveAutoDownloadSetting() {
    try {
      localStorage.setItem(STORAGE_KEYS.autoDownload, 'false');
    } catch {}
  }

  function openPreview(indexOrUrl = 0) {
    resetPreviewTransform();
    if (typeof indexOrUrl === 'string') {
      state.preview.urlMode = true;
      dom.previewImg.src = indexOrUrl;
      dom.previewNavPrev.hidden = true;
      dom.previewNavNext.hidden = true;
      dom.previewCounter.hidden = true;
    } else {
      state.preview.urlMode = false;
      state.preview.index = Math.min(Math.max(0, indexOrUrl), Math.max(0, state.gallery.length - 1));
      showPreviewImage(state.preview.index);
      dom.previewNavPrev.hidden = false;
      dom.previewNavNext.hidden = false;
      dom.previewCounter.hidden = false;
      updatePreviewNavigation();
    }
    dom.previewOverlay.classList.add('open');
  }

  function closePreview() {
    dom.previewOverlay.classList.remove('open');
    dom.previewImg.src = '';
  }

  function showPreviewImage(index) {
    const record = state.gallery[index];
    if (!record) return;
    state.preview.index = index;
    dom.previewImg.src = record.dataUrl;
    dom.previewCounter.textContent = `${index + 1} / ${state.gallery.length}`;
    updatePreviewNavigation();
  }

  function updatePreviewNavigation() {
    dom.previewNavPrev.classList.toggle('disabled', state.preview.index <= 0);
    dom.previewNavNext.classList.toggle('disabled', state.preview.index >= state.gallery.length - 1);
  }

  function prevImage() {
    if (state.preview.index > 0) {
      resetPreviewTransform();
      showPreviewImage(state.preview.index - 1);
    }
  }

  function nextImage() {
    if (state.preview.index < state.gallery.length - 1) {
      resetPreviewTransform();
      showPreviewImage(state.preview.index + 1);
    }
  }

  function resetPreviewTransform() {
    state.preview.scale = 1;
    state.preview.panX = 0;
    state.preview.panY = 0;
    updatePreviewTransform();
  }

  function updatePreviewTransform() {
    if (!dom.previewImg) return;
    dom.previewImg.style.transform = `translate(${state.preview.panX}px, ${state.preview.panY}px) scale(${state.preview.scale})`;
  }

  function handlePreviewWheel(event) {
    if (!dom.previewOverlay.classList.contains('open')) return;
    event.preventDefault();
    const delta = event.deltaY < 0 ? 0.12 : -0.12;
    state.preview.scale = Math.min(5, Math.max(0.4, state.preview.scale + delta));
    updatePreviewTransform();
  }

  function startPreviewDrag(event) {
    if (!dom.previewOverlay.classList.contains('open')) return;
    state.preview.dragging = true;
    state.preview.dragStartX = event.clientX;
    state.preview.dragStartY = event.clientY;
    state.preview.panStartX = state.preview.panX;
    state.preview.panStartY = state.preview.panY;
    dom.previewImg.classList.add('dragging');
  }

  function movePreviewDrag(event) {
    if (!state.preview.dragging) return;
    state.preview.panX = state.preview.panStartX + event.clientX - state.preview.dragStartX;
    state.preview.panY = state.preview.panStartY + event.clientY - state.preview.dragStartY;
    updatePreviewTransform();
  }

  function stopPreviewDrag() {
    state.preview.dragging = false;
    dom.previewImg?.classList.remove('dragging');
  }

  function handleGlobalKeydown(event) {
    if (!dom.previewOverlay.classList.contains('open')) return;
    if (event.key === 'Escape') closePreview();
    if (!state.preview.urlMode && event.key === 'ArrowLeft') prevImage();
    if (!state.preview.urlMode && event.key === 'ArrowRight') nextImage();
  }

  function initGuideBox() {
    if (!dom.guideBox || !dom.closeGuide) return;
    const hasShown = localStorage.getItem(STORAGE_KEYS.guideShown);
    dom.guideBox.hidden = Boolean(hasShown);
    dom.closeGuide.addEventListener('click', () => {
      dom.guideBox.hidden = true;
      localStorage.setItem(STORAGE_KEYS.guideShown, 'true');
    });
  }

  function createParticles() {
    if (!dom.particles || window.matchMedia('(prefers-reduced-motion: reduce)').matches) return;
    dom.particles.innerHTML = '';
    for (let index = 0; index < 28; index += 1) {
      const particle = document.createElement('span');
      particle.className = 'particle';
      particle.style.left = `${Math.random() * 100}%`;
      particle.style.top = `${Math.random() * 100}%`;
      particle.style.animationDelay = `${Math.random() * 8}s`;
      particle.style.animationDuration = `${10 + Math.random() * 14}s`;
      dom.particles.appendChild(particle);
    }
  }

  function structuredCloneSafe(value) {
    if (typeof structuredClone === 'function') return structuredClone(value);
    return JSON.parse(JSON.stringify(value));
  }
})();
