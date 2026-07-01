(() => {
  'use strict';

  const STORAGE_KEYS = {
    apiConfigs: 'img_gen_api_configs',
    activeApi: 'img_gen_active_api',
    promptHistory: 'img_gen_prompt_history',
    promptRecipes: 'img_gen_prompt_recipes',
    selectedStyleChips: 'img_gen_selected_style_chips',
    imageParams: 'img_gen_image_params',
    backgroundImage: 'img_gen_background_image',
    galleryLayout: 'img_gen_gallery_layout',
    galleryFavoritesOnly: 'img_gen_gallery_favorites_only',
    autoDownload: 'img_gen_auto_download',
  };

  const DB_NAME = 'img-gen-gallery';
  const DB_VERSION = 1;
  const STORE_NAME = 'records';
  const MAX_PROMPT_HISTORY = 20;
  const MAX_PROMPT_RECIPES = 20;
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

  const DEFAULT_CONFIGS = [DEFAULT_PICTURE_CONFIG];
  const REMOVED_DEFAULT_CONFIG_IDS = new Set(['default-openai', 'default-custom']);
  const PROMPT_STYLE_CHIPS = [
    {
      id: 'realistic-photo',
      label: '真实摄影',
      description: '真实人像、自然皮肤、摄影级光影',
      prompt: '真实摄影质感，皮肤和骨相保留自然细节，像高端人像摄影。',
    },
    {
      id: 'movie-still',
      label: '电影剧照',
      description: '像电影海报，画面有情绪和叙事',
      prompt: '电影剧照质感，镜头语言明确，像一帧决定性出场镜头。',
    },
    {
      id: 'oil-impasto',
      label: '油画厚涂',
      description: '背景和衣料更绘，但脸仍真实',
      prompt: '油画厚涂与手绘笔触更明显，背景和服装可以更绘画化，但人物脸保持真实可信。',
    },
    {
      id: 'oriental-fantasy',
      label: '东方奇幻',
      description: '云海、宫阙、花枝、神话氛围',
      prompt: '东方奇幻氛围，云海、宫阙、花枝、玉质与金属细节可以更梦幻。',
    },
    {
      id: 'future-sci-fi',
      label: '科幻未来',
      description: '未来建筑、透明材质、金属光泽',
      prompt: '科幻未来氛围，未来建筑、透明材质、金属光泽和环境发光可以更充分展开。',
    },
    {
      id: 'illustration-cover',
      label: '高级插画',
      description: '插画化背景，真人脸保持可信',
      prompt: '高级插画封面感，背景与服装可更绘画化，但人物脸必须保留真人结构和可信皮肤。',
    },
    {
      id: 'anime-cinema',
      label: '动画电影感',
      description: '色彩更梦幻，脸仍像真人',
      prompt: '动画电影感，背景和色彩可以更梦幻更漂亮，但人物脸仍要像真实世界里的人。',
    },
    {
      id: 'editorial-luxury',
      label: '时装大片',
      description: '高定、杂志、克制而压场',
      prompt: '高定时装大片感，姿态利落，服装与珠宝具备编辑大片质感。',
    },
    {
      id: 'vintage-film',
      label: '复古胶片',
      description: '胶片颗粒、旧时代故事',
      prompt: '复古胶片电影感，轻微颗粒、沉稳色调、年代故事感更强。',
    },
    {
      id: 'dreamscape',
      label: '梦幻风景',
      description: '背景特别好看，人物更突出',
      prompt: '梦幻风景氛围，背景可以极美、极丰富、极有想象力，但人物始终是绝对中心。',
    },
  ];
  const ADAPTIVE_THEME_VARIABLES = [
    '--bg',
    '--bg-soft',
    '--surface',
    '--surface-strong',
    '--line',
    '--line-strong',
    '--feature-line',
    '--feature-bg',
    '--feature-shadow',
    '--text',
    '--text-soft',
    '--muted',
    '--muted-2',
    '--primary',
    '--primary-2',
    '--accent',
    '--button-bg',
    '--button-shadow',
    '--control-bg',
    '--tabs-bg',
    '--tabs-glow',
    '--tool-bg',
    '--tool-border',
    '--top-divider',
    '--active-tab-bg',
    '--glass-glow-primary',
    '--glass-glow-accent',
    '--glass-wash',
    '--feature-glow',
    '--feature-inset',
    '--result-card-bg',
    '--result-card-shadow',
    '--progress-track',
    '--linework-color',
    '--linework-strong',
    '--linework-grid',
    '--linework-opacity',
    '--shadow',
    '--shadow-soft',
    '--focus',
  ];

  const PROMPT_VARIATION_AXES = {
    archetype: [
      '雨中江南电影美人感，透明伞、湿润花枝、银灰雨光，脸真实清透，一回眸就让人忘不掉',
      '暖光花海缪斯感，繁花、金色夕光和轻纱共同托住人物，长相真实但美得像命运安排',
      '东方科幻浮城女主感，远处巨构城池和云海极梦幻，但人物脸保持真实摄影级质感',
      '冰雪庭院宿命美人感，冷白雪林、晶莹发饰和安静凝视，脸部真实、清冷、难忘',
      '民国或旧上海电影感，正式剪裁、珍珠或丝绸细节，端庄里带一点不可言说的宿命',
      '高定杂志缪斯感，克制、锋利、干净，服装线条和人物骨相共同形成高级压场感',
      '海边暖光电影感，风吹发丝和衣料，人物明亮、温暖、真实，美得有呼吸感',
      '雨后城市长街美人感，湿润反光、柔和霓虹或路灯，脸和眼神仍是绝对中心',
      '欧洲庄园或剧院外景感，正式礼服、石阶或廊柱，人物像电影海报里的命运主角',
      '月夜花园幻想肖像感，花影、薄雾、微光和华丽衣饰可以梦幻，真人脸必须可信',
      '暖金油画花园肖像感，阳光、玫瑰、厚涂笔触和温柔回眸融合，但五官仍像真实人物',
    ],
    beauty: [
      '人物必须有倾国倾城的第一眼冲击力，五官精致但真实自然，气质比单纯漂亮更重要',
      '眼神要有一眼万年的记忆点，像命运在这一秒停住，观者会被她的视线吸住',
      '美感高级、克制、震撼，不要网红脸、廉价写真感、过度甜腻或塑料美颜',
      '脸部轮廓、眉眼、鼻梁、唇形和下颌线协调，整体像真实世界里极少见的绝世美人',
      '让她既有距离感又有真实情绪，像一个有故事、有身份、有宿命的人物',
      '美丽要带有灵魂感和压场感，不只是清晰好看，而是让画面有收藏价值',
      '人物气质干净、贵气、难忘，任何背景、服装和道具都只为她的存在感服务',
      '第一眼先被脸、眼神和气质震住，第二眼才看到服装、光线和空间细节',
      '避免普通模板脸，每一张脸都要有不同辨识度，保留真实皮肤纹理、轻微毛孔、自然不完美和细微表情',
      '整体效果像电影中女主角出场的决定性镜头，美得有故事、有重量、有命运感',
      '皮肤可以漂亮、干净、通透，但仍要像真实相机拍到的人，不要变成蜡像或瓷娃娃',
    ],
    framing: [
      '近景半身肖像，脸和眼神占据第一视觉中心，背景只做氛围衬托',
      '正面凝视构图，双眼清晰、瞳孔有高光，形成一眼万年的凝视感',
      '3/4 侧脸回眸，露出优雅下颌线和肩颈轮廓，画面有惊鸿一瞥的瞬间感',
      '低机位轻微仰拍，增强电影女主或高定缪斯般的气场，不夸张变形',
      '高机位安静俯拍，人物像被命运凝视，情绪更脆弱、更难忘',
      '中景全身构图，服装、发丝、手部动作和场景共同塑造绝世气质',
      '远景叙事构图，人物被宏大环境包围，但仍然是画面唯一灵魂',
      '居中对称构图，带仪式感、宿命感和封面级稳定性',
      '斜向动态构图，衣料、发丝和背景形成流动方向，让画面有生命力',
      '特写构图，聚焦眉眼、唇部、发丝边缘和皮肤质感，避免过度磨皮',
    ],
    lens: [
      '85mm 顶级人像镜头，浅景深，背景化成柔软光斑，脸部立体自然',
      '135mm 长焦压缩空间，让人物像从背景中被光单独托出',
      '50mm 电影标准镜头，自然、真实、克制，避免夸张网红透视',
      '35mm 环境人像镜头，人物和空间共同讲故事，但脸仍是焦点',
      '电影长镜头语言，前景有轻微虚化遮挡，层次像大银幕剧照',
      '高定杂志封面镜头，轮廓干净，姿态和服装线条有设计感',
      '古典肖像镜头，安静稳定，像博物馆收藏级人物画面',
      '舞台追光镜头，背景暗下去，人物被一束光准确捕捉',
      '梦幻柔焦镜头，边缘轻微柔化但五官和眼神保持清晰锐利',
      '大片级特写镜头，眼神、睫毛、唇部和发丝成为视觉锚点',
    ],
    lighting: [
      '柔和冷白侧光，脸部干净，轮廓有电影感和距离感',
      '金色逆光边缘光，发丝、睫毛、肩颈和衣料边缘发亮',
      '雨后反射光，眼神有湿润高光，背景有色彩但不过曝不抢脸',
      '室内窗边暖光，局部照亮脸和手，氛围亲密又像电影剧照',
      '阴天漫反射光，皮肤通透但保留真实纹理，整体清冷而震撼',
      '舞台或剧院追光，人物从暗背景中出现，像命运性登场',
      '清晨雾光，低对比、柔软、明亮，但五官和眼神保持清晰锐利',
      '窗边冷暖混合光，脸部有细腻明暗过渡，情绪更深',
      '高定棚拍柔光，妆容、衣料和皮肤质感高级稳定',
      '夕阳红金侧光，黑发、肤色和正式服装轮廓形成强烈记忆点',
    ],
    atmosphere: [
      '惊鸿一瞥的震撼感，像人群中只看一眼就再也忘不掉',
      '宿命感，像电影女主在关键剧情节点第一次转身',
      '遥远感与人间感并存，既美得不可轻易靠近，又有真实情绪',
      '清冷破碎感，安静、克制、眼神里有无法说出口的故事',
      '压场的女王气场，沉稳、高贵、危险但不夸张',
      '梦境感，柔雾、慢节奏、情绪含蓄，像回忆里最美的一秒',
      '东方静谧感，留白、风、布料和光影共同营造气息',
      '复古电影感，颗粒轻微，颜色沉稳，人物像旧胶片里的传奇',
      '现代都市冷艳感，清晰、锋利、有节奏，背景可有科幻或插画气息但脸保留真人质感',
      '花与风的灵动感，漂亮但不浅薄，甜美里保留成熟气质和灵魂感',
      '宏大幻想感，背景像动画电影或概念艺术般漂亮，但人物脸必须是真实人像',
      '油画般的温暖眷恋感，笔触、阳光和花园可以很绘画化，但眉眼与微笑要有真人温度',
    ],
    palette: [
      '自然肤色、黑发、暖白和柔金，明亮温暖但不廉价甜腻',
      '旧胶片琥珀、深褐、柔黑和奶油高光，复古但脸部清晰',
      '雨后蓝灰、暖路灯和低饱和红，冷暖对比明显，眼神高光突出',
      '象牙白、墨绿、珍珠灰和少量暗金，正式高级但保持克制',
      '海边浅蓝、沙色、白色衣料和金色夕光，空气感明亮干净',
      '花园绿色、浅粉、木色和柔白，背景退后，人物肤色更突出',
      '黑白灰主调，只保留唇色、肤色或一处服装色作为视觉锚点',
      '冬日银灰、冷白、深蓝和自然肤色，清冷但真实不仙侠化',
      '剧院酒红、暗木、金色边光和黑色正式服装，电影感更强',
      '东方科幻金、雾蓝、琥珀光和金属微光，背景梦幻宏大但肤色真实',
      '油画暖金、玫瑰粉、草木绿和奶油白，像黄昏花园里的厚涂肖像',
      '低饱和高级色，背景色彩可以华丽，但脸、眼神和气质永远是主色',
    ],
    scene: [
      '加入轻微风吹动发丝、衣袖、薄纱或华丽正式服装边缘，让人物像刚从画面里转身',
      '背景可选花枝、透明伞、纱帘、窗框、剧院幕布、长廊、玻璃反射或巨大城市轮廓作为层次',
      '可加入雨、雪、薄雾、花瓣、海风、云海、尘埃光粒或细小发光颗粒，让画面更梦幻',
      '人物有一个优雅动作：回头、抬眼、拢衣、扶伞、触碰花枝、站在门口或凝视远方',
      '使用纵深路径、门廊、台阶、长街、海岸线、山路、剧院座席或天空桥制造命运感',
      '背景可以是户外、室内、城市、庄园、剧院、花海、雪林、奇幻宫殿、科幻浮城或极简棚拍，选择最美的一种托住人物',
      '远处有灯火、水面反光、窗光、海面、雨痕、柔焦植物、巨型建筑或云层光束，形成深度而不是平铺背景',
      '画面必须有明确视觉锚点：眼神高光、脸部轮廓、手部动作、衣领线条、透明衣料或一处强光斑',
      '场景必须服务人物，不要让建筑、花草、装饰、风景抢走主体',
      '背景细节轻微虚化或克制留白，主体清晰，整体像海报或封面级人物图',
    ],
    background: [
      '银灰雨巷与江南庭院，湿润梅花、透明伞、水面反光和白墙黑瓦形成诗意层次',
      '金色花海与繁花草坡，前景花朵柔焦，夕阳穿过花枝，背景像梦里最温暖的一秒',
      '东方科幻天空城，云海、悬浮楼阁、金属桥、远处巨构和暖金雾光形成宏大风景',
      '冰雪森林与冷白宫廊，雪粒、冰晶发光、远处亭台和蓝白雾气制造清冷宿命感',
      '旧上海室内或剧院，暗木、绒幕、台灯、镜面反光和暖色追光形成复古电影空间',
      '海岸夕光与风暴云，远处海面反光、礁石、长风和金色云缝让背景有史诗感',
      '玻璃花房与雨后植物，水珠、藤蔓、柔焦花朵和窗外雾光让画面明亮又精致',
      '月夜庭院与发光花枝，冷月、薄雾、石阶、水池和微光花瓣形成轻幻想氛围',
      '未来东方长街，霓虹、灯笼、金属屋檐、雨水反光和远处高塔融合，但不压过人物脸',
      '极简暗场与一束命运追光，背景几乎退黑，只留下布料、珠宝和脸部轮廓的高级光影',
      '暖金油画花园，玫瑰、草木、远处建筑剪影和厚涂阳光融合，画面像高级手绘肖像',
    ],
    style: [
      '真人电影人像加幻想背景风格，脸部保持摄影真实，背景可以像动画电影概念图一样漂亮',
      '高定时装大片 editorial 风格，姿态、妆造、珠宝和服装都精致克制，服装可以更华丽',
      '复古胶片电影美人风格，像旧时代传奇女主的定格镜头，背景有年代空间和故事感',
      '东方奇幻电影海报风格，服装、建筑、云雾和光线可以充分发挥，但脸必须真实',
      '油画插画肖像风格，整体可有厚涂笔触、纸纹和手绘质感，但五官比例、眼神和皮肤光影要像真人',
      '明亮暖调封面肖像风格，温暖、干净、有高级审美，不像影楼模板',
      '雨后城市电影剧照风格，反光、湿润空气和眼神情绪共同建立故事',
      '科幻东方史诗风格，宏大背景、金属微光和奇观城市服务人物，不要让人物变成游戏建模',
      '冰雪梦幻肖像风格，冷光、雪粒、晶莹饰品和真实眼神共同制造震撼',
      '轻梦幻现实主义风格，真实人物配合梦幻空气感、花海、云海或发光粒子',
      '高级插画封面风格，构图和背景可以绘画化，人物脸部仍保持真实亚洲女性的可信细节',
      '收藏级艺术人像风格，像可用于封面、海报、画册或电影主视觉的完成作品',
    ],
    detail: [
      '强调眼神高光、睫毛、眉眼神态、皮肤真实纹理和发丝边缘',
      '强调脸部骨相自然精致，五官协调，不要网红模板脸或蛇精脸',
      '强调手部动作自然、肩颈线条舒展、姿态优雅不僵硬',
      '强调正式服装或幻想礼服的剪裁、衣料褶皱、珠宝、发饰和皮肤之间的材质对比',
      '强调微表情，不要夸张笑容，保留克制、心事和宿命感',
      '强调景深层次，前景、中景、背景都有清楚分工，风景可以很美，但主体永远最强',
      '强调真实脸部摄影质感，不要塑料皮肤、AI 过度锐化、假脸或廉价磨皮',
      '强调主体比例自然，脸、手、眼睛、牙齿、身体结构准确',
      '强调画面统一性，妆容、服装、场景、光线、色彩和梦幻背景都服务绝世气质',
      '强调最终作品像可收藏的封面级人物图，而不是普通生成图',
    ],
  };

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
    selectedStyleChipIds: new Set(),
    promptRecipes: [],
    refImages: [],
    promptHistory: [],
    gallery: [],
    currentResults: [],
    currentPage: 'draw',
    galleryDirty: false,
    streamTextBuffer: '',
    streamTextFlushPending: false,
    streamTextVersion: 0,
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
    },
    preview: {
      index: 0,
      urlMode: false,
      items: [],
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
    backgroundImage: '',
  };

  let dbPromise = null;
  let backgroundThemeRequestId = 0;

  document.addEventListener('DOMContentLoaded', init);

  async function init() {
    cacheDom();
    bindEvents();
    loadApiConfigs();
    loadPromptHistory();
    loadStyleChipSelection();
    loadPromptRecipes();
    loadImageParams();
    loadGalleryPreferences();
    loadAutoDownloadSetting();
    loadBackgroundImage();
    createParticles();
    updateNetworkStatusDisplay();
    renderAll();

    try {
      await loadGallery();
    } catch (error) {
      showStatus('err', `展馆加载失败：${error.message}`);
    }

    renderGalleryIfVisible();
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
      backgroundFileInput: byId('backgroundFileInput'),
      changeBackgroundBtn: byId('changeBackgroundBtn'),
      resetBackgroundBtn: byId('resetBackgroundBtn'),
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
      styleChipList: byId('styleChipList'),
      styleChipAllBtn: byId('styleChipAllBtn'),
      styleChipClearBtn: byId('styleChipClearBtn'),
      saveRecipeBtn: byId('saveRecipeBtn'),
      recipeList: byId('recipeList'),
      recipeEmpty: byId('recipeEmpty'),
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
      toggleDataManager: byId('toggleDataManager'),
      dataManagerContent: byId('dataManagerContent'),
      storageImageCount: byId('storageImageCount'),
      storageSize: byId('storageSize'),
      storageApiCount: byId('storageApiCount'),
      storagePromptCount: byId('storagePromptCount'),
      exportAllDataBtn: byId('exportAllDataBtn'),
      importDataBtn: byId('importDataBtn'),
      downloadAllImagesBtn: byId('downloadAllImagesBtn'),
      clearImagesBtn: byId('clearImagesBtn'),
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
      particles: byId('particles'),
    });
  }

  function bindEvents() {
    dom.topTabs.forEach((tab) => {
      if (tab.dataset.page) {
        tab.addEventListener('click', () => switchTab(tab.dataset.page));
      }
    });

    dom.changeBackgroundBtn?.addEventListener('click', () => dom.backgroundFileInput?.click());
    dom.backgroundFileInput?.addEventListener('change', handleBackgroundFileChange);
    dom.resetBackgroundBtn?.addEventListener('click', resetBackgroundImage);

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

    dom.promptHistoryBtn?.addEventListener('click', () => {
      dom.promptHistoryPanel.classList.toggle('open');
    });
    dom.styleChipAllBtn?.addEventListener('click', selectAllStyleChips);
    dom.styleChipClearBtn?.addEventListener('click', clearStyleChipSelection);
    dom.saveRecipeBtn?.addEventListener('click', saveCurrentRecipe);

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
    dom.toggleDataManager?.addEventListener('click', () => {
      dom.dataManagerContent.classList.toggle('collapsed');
      dom.toggleDataManager.classList.toggle('collapsed');
    });
    dom.exportAllDataBtn?.addEventListener('click', exportAllData);
    dom.importDataBtn?.addEventListener('click', () => dom.importFileInput?.click());
    dom.importFileInput?.addEventListener('change', importAllData);
    dom.downloadAllImagesBtn?.addEventListener('click', downloadAllImages);
    dom.clearImagesBtn?.addEventListener('click', clearAllImages);
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
    renderStyleChips();
    renderRecipeList();
    renderThumbnails();
    renderGalleryIfVisible();
    updateStorageInfo();
  }

  function switchTab(page) {
    state.currentPage = page;
    dom.topTabs.forEach((tab) => tab.classList.toggle('active', tab.dataset.page === page));
    dom.tabDraw?.classList.toggle('active', page === 'draw');
    dom.tabGallery?.classList.toggle('active', page === 'gallery');
    if (page === 'gallery') renderGalleryWithControls();
    if (page !== 'gallery') releaseGalleryDom();
  }

  function renderGalleryIfVisible() {
    if (state.currentPage === 'gallery') {
      renderGalleryWithControls();
      return;
    }

    state.galleryDirty = true;
    updateGallerySummary();
  }

  function updateGallerySummary() {
    if (dom.topGalleryBadge) {
      dom.topGalleryBadge.textContent = state.gallery.length ? `(${state.gallery.length})` : '';
    }
    updateGalleryStats();
  }

  function releaseGalleryDom() {
    if (!dom.galleryGrid?.childElementCount) return;
    dom.galleryGrid.innerHTML = '';
    state.galleryDirty = true;
  }

  function handleScroll() {
    $('.top-tabs')?.classList.toggle('scrolled', window.scrollY > 10);
  }

  async function handleBackgroundFileChange() {
    const file = dom.backgroundFileInput?.files?.[0];
    if (!file) return;

    if (!file.type.startsWith('image/')) {
      showStatus('err', '请选择图片文件作为背景');
      dom.backgroundFileInput.value = '';
      return;
    }

    try {
      const dataUrl = await fileToCompressedBackground(file);
      state.backgroundImage = dataUrl;
      saveBackgroundImage();
      applyBackgroundImage();
      showStatus('done', '背景图已更换');
    } catch (error) {
      showStatus('err', `背景图更换失败：${error.message}`);
    } finally {
      dom.backgroundFileInput.value = '';
    }
  }

  function loadBackgroundImage() {
    try {
      state.backgroundImage = localStorage.getItem(STORAGE_KEYS.backgroundImage) || '';
    } catch {
      state.backgroundImage = '';
    }
    applyBackgroundImage();
  }

  function saveBackgroundImage() {
    try {
      if (state.backgroundImage) {
        localStorage.setItem(STORAGE_KEYS.backgroundImage, state.backgroundImage);
      } else {
        localStorage.removeItem(STORAGE_KEYS.backgroundImage);
      }
    } catch (error) {
      throw new Error('背景图保存失败，图片可能过大');
    }
  }

  function applyBackgroundImage() {
    if (state.backgroundImage) {
      document.body.style.setProperty('--custom-bg-image', `url("${state.backgroundImage}")`);
      document.body.classList.add('custom-bg');
      applyAdaptiveThemeFromImage(state.backgroundImage);
    } else {
      document.body.style.removeProperty('--custom-bg-image');
      document.body.classList.remove('custom-bg');
      resetAdaptiveTheme();
    }
    if (dom.resetBackgroundBtn) {
      dom.resetBackgroundBtn.hidden = !state.backgroundImage;
    }
  }

  function applyAdaptiveThemeFromImage(dataUrl) {
    const requestId = ++backgroundThemeRequestId;
    analyzeBackgroundTheme(dataUrl)
      .then((theme) => {
        if (requestId !== backgroundThemeRequestId || state.backgroundImage !== dataUrl) return;
        applyAdaptiveTheme(theme);
      })
      .catch(() => {
        if (requestId !== backgroundThemeRequestId) return;
        resetAdaptiveTheme(false);
      });
  }

  function applyAdaptiveTheme(theme) {
    const rootStyle = document.documentElement.style;
    Object.entries(theme).forEach(([name, value]) => rootStyle.setProperty(name, value));
    document.body.classList.add('adaptive-theme');
  }

  function resetAdaptiveTheme(incrementRequest = true) {
    if (incrementRequest) backgroundThemeRequestId += 1;
    const rootStyle = document.documentElement.style;
    ADAPTIVE_THEME_VARIABLES.forEach((name) => rootStyle.removeProperty(name));
    document.body.classList.remove('adaptive-theme');
  }

  function resetBackgroundImage() {
    state.backgroundImage = '';
    saveBackgroundImage();
    applyBackgroundImage();
    showStatus('info', '已恢复默认背景');
  }

  function fileToCompressedBackground(file) {
    return new Promise((resolve, reject) => {
      const reader = new FileReader();
      reader.onerror = () => reject(reader.error || new Error('读取图片失败'));
      reader.onload = () => {
        const image = new Image();
        image.onerror = () => reject(new Error('图片无法解析'));
        image.onload = () => {
          try {
            const maxSide = 1920;
            const ratio = Math.min(1, maxSide / Math.max(image.naturalWidth, image.naturalHeight));
            const width = Math.max(1, Math.round(image.naturalWidth * ratio));
            const height = Math.max(1, Math.round(image.naturalHeight * ratio));
            const canvas = document.createElement('canvas');
            canvas.width = width;
            canvas.height = height;
            const ctx = canvas.getContext('2d');
            if (!ctx) throw new Error('当前浏览器不支持图片压缩');
            ctx.drawImage(image, 0, 0, width, height);
            resolve(canvas.toDataURL('image/jpeg', 0.82));
          } catch (error) {
            reject(error);
          }
        };
        image.src = String(reader.result || '');
      };
      reader.readAsDataURL(file);
    });
  }

  function analyzeBackgroundTheme(dataUrl) {
    return new Promise((resolve, reject) => {
      const image = new Image();
      image.onerror = () => reject(new Error('背景图片取色失败'));
      image.onload = () => {
        try {
          const sampleSize = 64;
          const canvas = document.createElement('canvas');
          canvas.width = sampleSize;
          canvas.height = sampleSize;
          const ctx = canvas.getContext('2d', { willReadFrequently: true });
          if (!ctx) throw new Error('当前浏览器不支持背景取色');
          ctx.drawImage(image, 0, 0, sampleSize, sampleSize);
          const pixels = ctx.getImageData(0, 0, sampleSize, sampleSize).data;
          const palette = sampleBackgroundPixels(pixels);
          resolve(buildAdaptiveTheme(palette));
        } catch (error) {
          reject(error);
        }
      };
      image.src = dataUrl;
    });
  }

  function sampleBackgroundPixels(pixels) {
    let red = 0;
    let green = 0;
    let blue = 0;
    let totalWeight = 0;
    let bestColor = null;
    let bestScore = -1;

    for (let index = 0; index < pixels.length; index += 16) {
      const alpha = pixels[index + 3] / 255;
      if (alpha < 0.2) continue;

      const r = pixels[index];
      const g = pixels[index + 1];
      const b = pixels[index + 2];
      const hsl = rgbToHsl(r, g, b);
      const luma = getLuminance(r, g, b);
      const edgePenalty = luma < 0.04 || luma > 0.96 ? 0.38 : 1;
      const weight = alpha * edgePenalty * (0.55 + hsl.s * 1.65);

      red += r * weight;
      green += g * weight;
      blue += b * weight;
      totalWeight += weight;

      const score = hsl.s * 1.6 + (1 - Math.abs(hsl.l - 0.52)) * 0.8 + edgePenalty * 0.2;
      if (score > bestScore && luma > 0.08 && luma < 0.92) {
        bestScore = score;
        bestColor = { r, g, b, hsl };
      }
    }

    if (!totalWeight) {
      return {
        average: { r: 234, g: 248, b: 255 },
        source: { h: 0.55, s: 0.42, l: 0.58 },
      };
    }

    const average = {
      r: Math.round(red / totalWeight),
      g: Math.round(green / totalWeight),
      b: Math.round(blue / totalWeight),
    };
    const averageHsl = rgbToHsl(average.r, average.g, average.b);
    const source = bestColor?.hsl || averageHsl;
    return { average, source };
  }

  function buildAdaptiveTheme({ average, source }) {
    const luma = getLuminance(average.r, average.g, average.b);
    const isDark = luma < 0.42;
    const hue = source.h;
    const saturation = clamp(Math.max(source.s, 0.28), 0.28, 0.76);
    const primary = hslToRgb(hue, saturation, isDark ? 0.62 : 0.52);
    const secondary = hslToRgb(hue + 0.1, clamp(saturation * 0.86 + 0.08, 0.3, 0.82), isDark ? 0.58 : 0.55);
    const accent = hslToRgb(hue + 0.33, clamp(saturation * 0.72 + 0.12, 0.3, 0.78), isDark ? 0.62 : 0.48);
    const tint = hslToRgb(hue, clamp(saturation * 0.38, 0.14, 0.42), isDark ? 0.18 : 0.93);
    const tintSoft = hslToRgb(hue + 0.04, clamp(saturation * 0.32, 0.12, 0.38), isDark ? 0.24 : 0.86);
    const shadow = hslToRgb(hue, clamp(saturation * 0.5, 0.18, 0.46), isDark ? 0.12 : 0.38);

    if (isDark) {
      return {
        '--bg': rgbToCss(tint),
        '--bg-soft': rgbToCss(tintSoft),
        '--surface': rgbaToCss(tint, 0.64),
        '--surface-strong': rgbaToCss(tintSoft, 0.78),
        '--line': rgbaToCss({ r: 255, g: 255, b: 255 }, 0.22),
        '--line-strong': rgbaToCss(primary, 0.46),
        '--feature-line': rgbaToCss(primary, 0.3),
        '--feature-bg': rgbaToCss({ r: 255, g: 255, b: 255 }, 0.09),
        '--feature-shadow': `0 14px 34px ${rgbaToCss(shadow, 0.32)}`,
        '--text': '#f7fbff',
        '--text-soft': '#dce9f1',
        '--muted': '#b7c8d3',
        '--muted-2': '#93a8b5',
        '--primary': rgbToCss(primary),
        '--primary-2': rgbToCss(secondary),
        '--accent': rgbToCss(accent),
        '--button-bg': `linear-gradient(135deg, ${rgbaToCss(primary, 0.96)}, ${rgbaToCss(secondary, 0.9)})`,
        '--button-shadow': `0 12px 30px ${rgbaToCss(primary, 0.28)}`,
        '--control-bg': rgbaToCss({ r: 255, g: 255, b: 255 }, 0.12),
        '--tabs-bg': rgbaToCss(tint, 0.76),
        '--tabs-glow': rgbaToCss(primary, 0.2),
        '--tool-bg': rgbaToCss({ r: 255, g: 255, b: 255 }, 0.11),
        '--tool-border': rgbaToCss(primary, 0.28),
        '--top-divider': rgbaToCss({ r: 255, g: 255, b: 255 }, 0.22),
        '--active-tab-bg': `linear-gradient(135deg, ${rgbaToCss(primary, 0.94)}, ${rgbaToCss(secondary, 0.9)})`,
        '--glass-glow-primary': rgbaToCss(primary, 0.2),
        '--glass-glow-accent': rgbaToCss(accent, 0.16),
        '--glass-wash': `linear-gradient(135deg, ${rgbaToCss({ r: 255, g: 255, b: 255 }, 0.12)}, ${rgbaToCss(primary, 0.13)} 48%, ${rgbaToCss(secondary, 0.1)})`,
        '--feature-glow': rgbaToCss(primary, 0.18),
        '--feature-inset': rgbaToCss({ r: 255, g: 255, b: 255 }, 0.14),
        '--result-card-bg': `linear-gradient(180deg, ${rgbaToCss({ r: 255, g: 255, b: 255 }, 0.14)}, ${rgbaToCss(secondary, 0.14)}), ${rgbaToCss(tint, 0.66)}`,
        '--result-card-shadow': `0 18px 42px ${rgbaToCss(shadow, 0.34)}`,
        '--progress-track': rgbaToCss(secondary, 0.2),
        '--linework-color': rgbaToCss({ r: 255, g: 255, b: 255 }, 0.08),
        '--linework-strong': rgbaToCss(primary, 0.2),
        '--linework-grid': rgbaToCss({ r: 255, g: 255, b: 255 }, 0.055),
        '--linework-opacity': '0.74',
        '--shadow': `0 22px 70px ${rgbaToCss(shadow, 0.38)}`,
        '--shadow-soft': `0 14px 36px ${rgbaToCss(shadow, 0.3)}`,
        '--focus': `0 0 0 3px ${rgbaToCss(secondary, 0.32)}`,
      };
    }

    return {
      '--bg': rgbToCss(tint),
      '--bg-soft': rgbToCss(tintSoft),
      '--surface': rgbaToCss(tint, 0.64),
      '--surface-strong': rgbaToCss({ r: 255, g: 255, b: 255 }, 0.84),
      '--line': rgbaToCss(primary, 0.24),
      '--line-strong': rgbaToCss(primary, 0.48),
      '--feature-line': rgbaToCss(secondary, 0.34),
      '--feature-bg': rgbaToCss({ r: 255, g: 255, b: 255 }, 0.54),
      '--feature-shadow': `0 14px 34px ${rgbaToCss(shadow, 0.12)}`,
      '--text': '#102a43',
      '--text-soft': '#28465e',
      '--muted': '#557084',
      '--muted-2': '#748ca0',
      '--primary': rgbToCss(primary),
      '--primary-2': rgbToCss(secondary),
      '--accent': rgbToCss(accent),
      '--button-bg': `linear-gradient(135deg, ${rgbaToCss(primary, 0.96)}, ${rgbaToCss(secondary, 0.92)})`,
      '--button-shadow': `0 12px 28px ${rgbaToCss(secondary, 0.24)}`,
      '--control-bg': rgbaToCss({ r: 255, g: 255, b: 255 }, 0.68),
      '--tabs-bg': rgbaToCss({ r: 250, g: 254, b: 255 }, 0.68),
      '--tabs-glow': rgbaToCss(primary, 0.18),
      '--tool-bg': rgbaToCss({ r: 255, g: 255, b: 255 }, 0.42),
      '--tool-border': rgbaToCss(secondary, 0.26),
      '--top-divider': rgbaToCss(secondary, 0.22),
      '--active-tab-bg': `linear-gradient(135deg, ${rgbaToCss(primary, 0.92)}, ${rgbaToCss(secondary, 0.9)})`,
      '--glass-glow-primary': rgbaToCss(primary, 0.2),
      '--glass-glow-accent': rgbaToCss(accent, 0.18),
      '--glass-wash': `linear-gradient(135deg, ${rgbaToCss({ r: 255, g: 255, b: 255 }, 0.72)}, ${rgbaToCss(secondary, 0.22)} 48%, ${rgbaToCss(primary, 0.18)})`,
      '--feature-glow': rgbaToCss(primary, 0.14),
      '--feature-inset': rgbaToCss({ r: 255, g: 255, b: 255 }, 0.78),
      '--result-card-bg': `linear-gradient(180deg, ${rgbaToCss({ r: 255, g: 255, b: 255 }, 0.76)}, ${rgbaToCss(secondary, 0.2)}), ${rgbaToCss({ r: 255, g: 255, b: 255 }, 0.68)}`,
      '--result-card-shadow': `0 18px 42px ${rgbaToCss(shadow, 0.18)}`,
      '--progress-track': rgbaToCss(secondary, 0.18),
      '--linework-color': rgbaToCss(secondary, 0.1),
      '--linework-strong': rgbaToCss(primary, 0.15),
      '--linework-grid': rgbaToCss(secondary, 0.07),
      '--linework-opacity': '0.8',
      '--shadow': `0 22px 70px ${rgbaToCss(shadow, 0.22)}`,
      '--shadow-soft': `0 14px 36px ${rgbaToCss(shadow, 0.16)}`,
      '--focus': `0 0 0 3px ${rgbaToCss(secondary, 0.28)}`,
    };
  }

  function rgbToHsl(r, g, b) {
    const red = r / 255;
    const green = g / 255;
    const blue = b / 255;
    const max = Math.max(red, green, blue);
    const min = Math.min(red, green, blue);
    const lightness = (max + min) / 2;
    if (max === min) return { h: 0, s: 0, l: lightness };

    const delta = max - min;
    const saturation = lightness > 0.5 ? delta / (2 - max - min) : delta / (max + min);
    let hue = 0;
    if (max === red) hue = (green - blue) / delta + (green < blue ? 6 : 0);
    else if (max === green) hue = (blue - red) / delta + 2;
    else hue = (red - green) / delta + 4;

    return { h: hue / 6, s: saturation, l: lightness };
  }

  function hslToRgb(h, s, l) {
    const hue = ((h % 1) + 1) % 1;
    if (s === 0) {
      const value = Math.round(l * 255);
      return { r: value, g: value, b: value };
    }

    const q = l < 0.5 ? l * (1 + s) : l + s - l * s;
    const p = 2 * l - q;
    return {
      r: Math.round(hueToRgb(p, q, hue + 1 / 3) * 255),
      g: Math.round(hueToRgb(p, q, hue) * 255),
      b: Math.round(hueToRgb(p, q, hue - 1 / 3) * 255),
    };
  }

  function hueToRgb(p, q, t) {
    let hue = t;
    if (hue < 0) hue += 1;
    if (hue > 1) hue -= 1;
    if (hue < 1 / 6) return p + (q - p) * 6 * hue;
    if (hue < 1 / 2) return q;
    if (hue < 2 / 3) return p + (q - p) * (2 / 3 - hue) * 6;
    return p;
  }

  function getLuminance(r, g, b) {
    return (0.2126 * r + 0.7152 * g + 0.0722 * b) / 255;
  }

  function rgbToCss(color) {
    return `rgb(${color.r}, ${color.g}, ${color.b})`;
  }

  function rgbaToCss(color, alpha) {
    return `rgba(${color.r}, ${color.g}, ${color.b}, ${alpha})`;
  }

  function clamp(value, min, max) {
    return Math.min(max, Math.max(min, value));
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

    state.apiConfigs = removeRemovedDefaultConfigs(state.apiConfigs);

    if (!Array.isArray(state.apiConfigs) || state.apiConfigs.length === 0) {
      state.apiConfigs = structuredCloneSafe(DEFAULT_CONFIGS);
    }

    ensureDefaultPictureConfig();

    if (!state.apiConfigs.some((config) => config.id === state.activeApiId)) {
      state.activeApiId = state.apiConfigs[0]?.id || null;
    }

    if (!state.activeApiId || REMOVED_DEFAULT_CONFIG_IDS.has(state.activeApiId)) {
      state.activeApiId = DEFAULT_PICTURE_CONFIG.id;
    }

    saveApiConfigs();
  }

  function removeRemovedDefaultConfigs(configs) {
    if (!Array.isArray(configs)) return [];
    return configs.filter((config) => !REMOVED_DEFAULT_CONFIG_IDS.has(config?.id));
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

  function loadStyleChipSelection() {
    try {
      const saved = JSON.parse(localStorage.getItem(STORAGE_KEYS.selectedStyleChips) || '[]');
      state.selectedStyleChipIds = new Set(normalizeStyleChipIds(saved));
    } catch {
      state.selectedStyleChipIds = new Set();
    }
  }

  function saveStyleChipSelection() {
    try {
      localStorage.setItem(STORAGE_KEYS.selectedStyleChips, JSON.stringify([...state.selectedStyleChipIds]));
    } catch {}
  }

  function renderStyleChips() {
    if (!dom.styleChipList) return;
    dom.styleChipList.innerHTML = '';

    PROMPT_STYLE_CHIPS.forEach((chip) => {
      const button = document.createElement('button');
      button.type = 'button';
      button.className = 'style-chip';
      button.setAttribute('aria-pressed', String(state.selectedStyleChipIds.has(chip.id)));
      button.title = chip.description;

      const label = createTextElement('span', 'style-chip-label', chip.label);
      const desc = createTextElement('small', 'style-chip-desc', chip.description);
      button.append(label, desc);
      button.addEventListener('click', () => toggleStyleChip(chip.id));
      dom.styleChipList.appendChild(button);
    });

    if (dom.styleChipClearBtn) dom.styleChipClearBtn.disabled = state.selectedStyleChipIds.size === 0;
    if (dom.styleChipAllBtn) dom.styleChipAllBtn.disabled = state.selectedStyleChipIds.size === PROMPT_STYLE_CHIPS.length;
  }

  function toggleStyleChip(id) {
    if (state.selectedStyleChipIds.has(id)) {
      state.selectedStyleChipIds.delete(id);
    } else {
      state.selectedStyleChipIds.add(id);
    }
    saveStyleChipSelection();
    renderStyleChips();
  }

  function selectAllStyleChips() {
    state.selectedStyleChipIds = new Set(PROMPT_STYLE_CHIPS.map((chip) => chip.id));
    saveStyleChipSelection();
    renderStyleChips();
  }

  function clearStyleChipSelection() {
    state.selectedStyleChipIds.clear();
    saveStyleChipSelection();
    renderStyleChips();
  }

  function normalizeStyleChipIds(ids) {
    if (!Array.isArray(ids)) return [];
    const validIds = new Set(PROMPT_STYLE_CHIPS.map((chip) => chip.id));
    return [...new Set(ids.filter((id) => validIds.has(id)))];
  }

  function getStyleChipsByIds(ids) {
    const normalizedIds = normalizeStyleChipIds(ids);
    return normalizedIds
      .map((id) => PROMPT_STYLE_CHIPS.find((chip) => chip.id === id))
      .filter(Boolean);
  }

  function getSelectedStyleChips() {
    return getStyleChipsByIds([...state.selectedStyleChipIds]);
  }

  function getStyleSelectionPool() {
    const selected = getSelectedStyleChips();
    return selected.length ? selected : PROMPT_STYLE_CHIPS;
  }

  function pickStyleChipsForVariation(index, batchSeed = 0) {
    const pool = getStyleSelectionPool();
    if (!pool.length) return [];
    const seedOffset = Math.abs(Math.floor(Number(batchSeed) || 0)) % pool.length;
    const count = Math.min(pool.length, state.selectedStyleChipIds.size > 1 ? 2 : 1);
    const chips = [];

    for (let offset = 0; chips.length < count && offset < pool.length; offset += 1) {
      const chip = pool[(index * 3 + seedOffset + offset * 5) % pool.length];
      if (!chips.some((item) => item.id === chip.id)) chips.push(chip);
    }

    return chips;
  }

  function getStyleChipMetadata(chips = getSelectedStyleChips()) {
    return {
      styleChipIds: chips.map((chip) => chip.id),
      styleChipLabels: chips.map((chip) => chip.label),
    };
  }

  function loadPromptRecipes() {
    try {
      const saved = JSON.parse(localStorage.getItem(STORAGE_KEYS.promptRecipes) || '[]');
      state.promptRecipes = Array.isArray(saved)
        ? saved.map(normalizePromptRecipe).filter(Boolean).slice(0, MAX_PROMPT_RECIPES)
        : [];
    } catch {
      state.promptRecipes = [];
    }
  }

  function savePromptRecipes() {
    try {
      localStorage.setItem(STORAGE_KEYS.promptRecipes, JSON.stringify(state.promptRecipes));
    } catch {}
  }

  function normalizePromptRecipe(recipe, index = 0) {
    if (!recipe || typeof recipe !== 'object') return null;
    const prompt = String(recipe.prompt || '').trim();
    if (!prompt) return null;
    const styleChipIds = normalizeStyleChipIds(recipe.styleChipIds);
    const styleChipLabels = getStyleChipsByIds(styleChipIds).map((chip) => chip.label);
    return {
      id: String(recipe.id || `recipe-${Date.now()}-${index}`),
      name: String(recipe.name || buildRecipeName(prompt, styleChipLabels)).slice(0, 64),
      prompt,
      styleChipIds,
      styleChipLabels,
      params: recipe.params && typeof recipe.params === 'object' ? { ...recipe.params } : {},
      createdAt: recipe.createdAt || new Date().toISOString(),
    };
  }

  function buildRecipeName(prompt, styleLabels = []) {
    const prefix = styleLabels.length ? `${styleLabels.slice(0, 2).join(' / ')} · ` : '';
    const text = String(prompt || '').replace(/\s+/g, ' ').trim();
    return `${prefix}${text.slice(0, 26) || '未命名配方'}`;
  }

  function saveCurrentRecipe() {
    const prompt = dom.prompt?.value?.trim() || '';
    if (!prompt) {
      showStatus('err', '请先填写提示词，再保存配方');
      dom.prompt?.focus();
      return;
    }

    const selectedChips = getSelectedStyleChips();
    const metadata = getStyleChipMetadata(selectedChips);
    const recipe = normalizePromptRecipe({
      id: `recipe-${Date.now()}`,
      name: buildRecipeName(prompt, metadata.styleChipLabels),
      prompt,
      ...metadata,
      params: getImageParams(),
      createdAt: new Date().toISOString(),
    });

    state.promptRecipes = [
      recipe,
      ...state.promptRecipes.filter((item) => item.prompt !== recipe.prompt || item.styleChipIds.join('|') !== recipe.styleChipIds.join('|')),
    ].slice(0, MAX_PROMPT_RECIPES);
    savePromptRecipes();
    renderRecipeList();
    showStatus('done', '当前提示词配方已保存');
  }

  function renderRecipeList() {
    if (!dom.recipeList || !dom.recipeEmpty) return;
    dom.recipeList.innerHTML = '';
    dom.recipeEmpty.hidden = state.promptRecipes.length > 0;

    state.promptRecipes.forEach((recipe) => {
      const item = document.createElement('article');
      item.className = 'recipe-card';
      const title = createTextElement('strong', 'recipe-title', recipe.name);
      const meta = createTextElement('span', 'recipe-meta', recipe.styleChipLabels.length ? recipe.styleChipLabels.join(' / ') : '随机风格池');
      const prompt = createTextElement('p', 'recipe-text', recipe.prompt);
      const actions = document.createElement('div');
      actions.className = 'recipe-actions';

      const useBtn = createTextElement('button', '', '使用');
      useBtn.type = 'button';
      useBtn.addEventListener('click', () => applyPromptRecipe(recipe.id));
      const delBtn = createTextElement('button', 'danger', '删除');
      delBtn.type = 'button';
      delBtn.addEventListener('click', () => deletePromptRecipe(recipe.id));
      actions.append(useBtn, delBtn);
      item.append(title, meta, prompt, actions);
      dom.recipeList.appendChild(item);
    });
  }

  function applyPromptRecipe(id) {
    const recipe = state.promptRecipes.find((item) => item.id === id);
    if (!recipe) return;
    dom.prompt.value = recipe.prompt;
    state.selectedStyleChipIds = new Set(normalizeStyleChipIds(recipe.styleChipIds));
    state.imageParams = { ...state.imageParams, ...(recipe.params || {}) };
    saveStyleChipSelection();
    saveImageParams();
    loadImageParams();
    renderStyleChips();
    dom.prompt.focus();
    showStatus('done', '已载入提示词配方');
  }

  function deletePromptRecipe(id) {
    state.promptRecipes = state.promptRecipes.filter((item) => item.id !== id);
    savePromptRecipes();
    renderRecipeList();
  }

  function loadGalleryPreferences() {
    try {
      localStorage.removeItem(STORAGE_KEYS.galleryLayout);
      localStorage.removeItem(STORAGE_KEYS.galleryFavoritesOnly);
    } catch {}
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
    state.gallery = records
      .map(normalizeGalleryRecord)
      .filter(Boolean)
      .sort((a, b) => new Date(b.createdAt || b.time || 0) - new Date(a.createdAt || a.time || 0));
  }

  function normalizeGalleryRecord(record, index = 0) {
    if (!record || typeof record !== 'object') return null;
    const normalized = { ...record };
    normalized.id = normalized.id ?? `legacy-${Date.now()}-${index}`;
    normalized.prompt = String(normalized.prompt || '');
    normalized.sourcePrompt = String(normalized.sourcePrompt || extractCorePrompt(normalized.prompt) || normalized.prompt);
    normalized.mode = Number(normalized.mode) === 2 ? 2 : 1;
    normalized.refDataUrl = normalized.refDataUrl || null;
    normalized.createdAt = normalized.createdAt || new Date().toISOString();
    normalized.time = normalized.time || new Date(normalized.createdAt).toLocaleString();
    normalized.params = normalized.params && typeof normalized.params === 'object' ? normalized.params : {};
    delete normalized.rating;
    delete normalized.favoriteRank;
    normalized.styleChipIds = normalizeStyleChipIds(normalized.styleChipIds);
    normalized.styleChipLabels = Array.isArray(normalized.styleChipLabels) && normalized.styleChipLabels.length
      ? normalized.styleChipLabels.filter(Boolean).slice(0, 4)
      : getStyleChipsByIds(normalized.styleChipIds).map((chip) => chip.label);
    return normalized;
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
      renderGalleryIfVisible();
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

  async function addToGallery(dataUrl, prompt, mode, refDataUrl, metadata = {}) {
    const now = new Date();
    const record = normalizeGalleryRecord({
      id: Date.now() + Math.floor(Math.random() * 1000),
      dataUrl,
      prompt,
      sourcePrompt: metadata.sourcePrompt || extractCorePrompt(prompt) || prompt,
      mode,
      refDataUrl: refDataUrl || null,
      createdAt: now.toISOString(),
      time: now.toLocaleString(),
      params: getImageParams(),
      styleChipIds: metadata.styleChipIds || [],
      styleChipLabels: metadata.styleChipLabels || [],
      recipeSnapshot: metadata.recipeSnapshot || null,
    });
    await saveRecord(record);
    state.gallery.unshift(record);
    renderGalleryIfVisible();
    updateStorageInfo();
    return record;
  }

  async function deleteFromGallery(id) {
    try {
      await deleteRecord(id);
      state.gallery = state.gallery.filter((item) => item.id !== id);
      renderGalleryIfVisible();
      updateStorageInfo();
      showStatus('info', '图片记录已删除');
    } catch (error) {
      showStatus('err', `删除失败：${error.message}`);
    }
  }

  function renderGalleryWithControls() {
    if (!dom.galleryGrid) return;
    state.galleryDirty = false;
    const filtered = sortGallery([...state.gallery]);
    dom.galleryGrid.innerHTML = '';
    dom.galleryGrid.classList.remove('masonry-layout');

    if (!filtered.length) {
      dom.galleryEmpty.hidden = false;
      dom.galleryEmpty.textContent = '暂无生成记录，快去生成第一张图片吧。';
      dom.galleryCount.textContent = '';
      dom.topGalleryBadge.textContent = state.gallery.length ? `(${state.gallery.length})` : '';
      updateGalleryStats();
      return;
    }

    dom.galleryEmpty.hidden = true;
    dom.galleryCount.textContent = `(${filtered.length} 张)`;
    dom.topGalleryBadge.textContent = `(${state.gallery.length})`;

    filtered.forEach((record) => dom.galleryGrid.appendChild(createGalleryCard(record, filtered)));

    updateGalleryStats();
  }

  function sortGallery(items) {
    return items.sort((a, b) => new Date(b.createdAt || b.time || 0) - new Date(a.createdAt || a.time || 0));
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

  function createGalleryCard(record, previewItems = state.gallery) {
    const card = document.createElement('article');
    card.className = `gallery-item ${state.galleryView.displayMode === 'card' ? 'card-mode' : 'normal-mode'}`;

    const thumb = document.createElement('div');
    thumb.className = 'thumb-wrap';
    const image = createGalleryImage(record);
    image.classList.add('gallery-image');

    if (state.galleryView.displayMode === 'card') {
      const inner = document.createElement('div');
      inner.className = 'flip-card-inner';
      const front = document.createElement('div');
      front.className = 'flip-card-front';
      front.appendChild(image);
      front.appendChild(createTextElement('span', 'card-back-symbol', 'AI'));
      inner.appendChild(front);
      thumb.appendChild(inner);
    } else {
      image.classList.add('normal-image');
      thumb.appendChild(image);
    }

    thumb.addEventListener('click', () => {
      const imageIndex = previewItems.findIndex((item) => item.id === record.id);
      openPreviewList(previewItems, imageIndex >= 0 ? imageIndex : 0);
    });
    thumb.appendChild(createTextElement('span', 'mode-badge', record.mode === 2 ? '图生图' : '文生图'));
    card.appendChild(thumb);

    const info = document.createElement('div');
    info.className = 'info';

    const summary = document.createElement('div');
    summary.className = 'gallery-summary';
    const tags = generatePromptTags(record.prompt);
    if (tags.length) {
      const tagsWrap = document.createElement('div');
      tagsWrap.className = 'prompt-tags';
      tags.forEach((tag) => tagsWrap.appendChild(createTextElement('span', 'prompt-tag', tag)));
      summary.appendChild(tagsWrap);
    }
    const timeEl = createTextElement('time', 'gallery-time', formatGalleryTime(record));
    if (record.createdAt) timeEl.dateTime = record.createdAt;
    summary.appendChild(timeEl);
    info.appendChild(summary);

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

    if (record.styleChipLabels?.length) {
      const styleRow = document.createElement('div');
      styleRow.className = 'gallery-style-row';
      record.styleChipLabels.slice(0, 4).forEach((label) => {
        styleRow.appendChild(createTextElement('span', 'gallery-style-pill', label));
      });
      info.appendChild(styleRow);
    }

    const promptId = `gallery-prompt-${record.id}`;
    const toggleBtn = createTextElement('button', 'prompt-toggle-btn', '查看提示词');
    toggleBtn.type = 'button';
    toggleBtn.setAttribute('aria-expanded', 'false');
    toggleBtn.setAttribute('aria-controls', promptId);

    const actions = document.createElement('div');
    actions.className = 'gallery-actions';
    const backgroundBtn = createTextElement('button', 'set-bg-btn', '设为背景');
    backgroundBtn.type = 'button';
    backgroundBtn.addEventListener('click', (event) => {
      event.stopPropagation();
      setRecordAsBackground(record.id);
    });
    const variantBtn = createTextElement('button', 'variant-btn', '再生变体');
    variantBtn.type = 'button';
    variantBtn.addEventListener('click', (event) => {
      event.stopPropagation();
      regenerateVariant(record.id);
    });
    const delBtn = createTextElement('button', 'del-btn', '删除');
    delBtn.type = 'button';
    delBtn.addEventListener('click', (event) => {
      event.stopPropagation();
      deleteFromGallery(record.id);
    });
    actions.append(toggleBtn, backgroundBtn, variantBtn, delBtn);

    const promptPanel = document.createElement('div');
    promptPanel.id = promptId;
    promptPanel.className = 'gallery-prompt-panel';
    promptPanel.hidden = true;

    const promptHeader = document.createElement('div');
    promptHeader.className = 'gallery-prompt-header';
    promptHeader.appendChild(createTextElement('span', '', '提示词'));
    const copyBtn = createTextElement('button', 'copy-prompt', '复制提示词');
    copyBtn.type = 'button';
    copyBtn.addEventListener('click', (event) => {
      event.stopPropagation();
      copyText(record.prompt, '提示词已复制');
    });
    promptHeader.appendChild(copyBtn);

    const promptEl = createTextElement('p', 'prompt-text', record.prompt || '这条记录没有保存提示词。');
    promptPanel.append(promptHeader, promptEl);

    toggleBtn.addEventListener('click', (event) => {
      event.stopPropagation();
      promptPanel.hidden = !promptPanel.hidden;
      const expanded = !promptPanel.hidden;
      toggleBtn.textContent = expanded ? '收起提示词' : '查看提示词';
      toggleBtn.setAttribute('aria-expanded', String(expanded));
    });

    info.append(actions, promptPanel);
    card.appendChild(info);
    return card;
  }

  async function setRecordAsBackground(id) {
    const record = state.gallery.find((item) => item.id === id);
    if (!record?.dataUrl) return;
    try {
      state.backgroundImage = await compressDataUrlForBackground(record.dataUrl);
      saveBackgroundImage();
      applyBackgroundImage();
      showStatus('done', '已把这张图设为界面背景');
    } catch (error) {
      showStatus('err', `设置背景失败：${error.message}`);
    }
  }

  function compressDataUrlForBackground(dataUrl) {
    return new Promise((resolve, reject) => {
      const image = new Image();
      image.onerror = () => reject(new Error('图片无法作为背景读取'));
      image.onload = () => {
        try {
          const maxSide = 1920;
          const ratio = Math.min(1, maxSide / Math.max(image.naturalWidth, image.naturalHeight));
          const width = Math.max(1, Math.round(image.naturalWidth * ratio));
          const height = Math.max(1, Math.round(image.naturalHeight * ratio));
          const canvas = document.createElement('canvas');
          canvas.width = width;
          canvas.height = height;
          const ctx = canvas.getContext('2d');
          if (!ctx) throw new Error('当前浏览器不支持背景压缩');
          ctx.drawImage(image, 0, 0, width, height);
          resolve(canvas.toDataURL('image/jpeg', 0.82));
        } catch (error) {
          reject(error);
        }
      };
      image.src = dataUrl;
    });
  }

  function regenerateVariant(id) {
    if (state.generation.active) {
      showStatus('info', '正在生成中，完成后再生变体');
      return;
    }
    const record = state.gallery.find((item) => item.id === id);
    if (!record) return;
    dom.prompt.value = buildVariantPromptForRecord(record);
    if (record.styleChipIds?.length) {
      state.selectedStyleChipIds = new Set(normalizeStyleChipIds(record.styleChipIds));
      saveStyleChipSelection();
      renderStyleChips();
    }
    setSelectedGenCount(3);
    switchTab('draw');
    showStatus('info', '已基于这张图生成变体提示词，开始生成 3 张变体');
    window.setTimeout(() => generate(), 60);
  }

  function buildVariantPromptForRecord(record) {
    const sourcePrompt = getRecordSourcePrompt(record);
    const styleLabels = record.styleChipLabels?.length ? record.styleChipLabels.join(' / ') : '从全部风格池中随机挑选';
    return [
      '基于这张已选图片的成功方向，重新生成同主题的变化版本。',
      `核心主题：${sourcePrompt}`,
      `沿用或参考风格：${styleLabels}`,
      '变体要求：不要复制原图，要明显微调镜头距离、光线方向、背景风景、服装细节和人物情绪。',
      '人物底线：成年真实亚洲女性，脸部必须真实、有辨识度、有一眼万年的惊艳感；背景和服装可以更梦幻、更电影、更插画化，但脸和眼神必须像真实人物。',
      '出片目标：每一张都像封面、电影海报或收藏级人物图，风景更好看，人物仍是绝对视觉中心。',
    ].join('\n');
  }

  function getRecordSourcePrompt(record) {
    return String(record.sourcePrompt || extractCorePrompt(record.prompt) || record.prompt || '').trim();
  }

  function extractCorePrompt(prompt) {
    const text = String(prompt || '').trim();
    const match = text.match(/核心主题[:：]\s*([^\n]+)/);
    return (match?.[1] || text).trim();
  }

  function setSelectedGenCount(count) {
    state.selectedGenCount = Number(count) || 1;
    $$('.gen-count-btn').forEach((button) => {
      button.classList.toggle('active', Number(button.dataset.count || 1) === state.selectedGenCount);
    });
  }

  function createGalleryImage(record) {
    const image = document.createElement('img');
    image.src = record.dataUrl;
    image.alt = record.mode === 2 ? '图生图生成图片' : '文生图生成图片';
    image.loading = 'lazy';
    image.addEventListener('error', () => {
      image.replaceWith(createBrokenImageNotice(record));
    }, { once: true });
    return image;
  }

  function formatGalleryTime(record) {
    if (record.createdAt) {
      const date = new Date(record.createdAt);
      if (!Number.isNaN(date.getTime())) {
        return date.toLocaleString('zh-CN', {
          year: 'numeric',
          month: '2-digit',
          day: '2-digit',
          hour: '2-digit',
          minute: '2-digit',
        });
      }
    }
    return record.time || '-';
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
    if (dom.text2imgCount) dom.text2imgCount.textContent = `${text2imgCount} 张`;
    if (dom.img2imgCount) dom.img2imgCount.textContent = `${img2imgCount} 张`;
    if (dom.totalCount) dom.totalCount.textContent = `${state.gallery.length} 张`;
  }

  function updateStorageInfo() {
    if (!dom.storageImageCount) return;
    dom.storageImageCount.textContent = `${state.gallery.length} 张`;
    dom.storageApiCount.textContent = `${state.apiConfigs.length} 个`;
    dom.storagePromptCount.textContent = `${state.promptHistory.length} 条`;
    if (dom.clearImagesBtn) dom.clearImagesBtn.disabled = state.gallery.length === 0;
    const totalBytes = state.gallery.reduce((total, item) => {
      const imageSize = item.dataUrl ? item.dataUrl.length * 0.75 : 0;
      const refSize = item.refDataUrl ? item.refDataUrl.length * 0.75 : 0;
      return total + imageSize + refSize;
    }, 0);
    dom.storageSize.textContent = `${(totalBytes / (1024 * 1024)).toFixed(2)} MB`;
  }

  function resetCurrentRun() {
    state.streamTextBuffer = '';
    state.streamTextFlushPending = false;
    state.streamTextVersion += 1;
    if (dom.eventLog) {
      dom.eventLog.innerHTML = '';
      dom.eventLog.classList.remove('active');
    }
    if (dom.textStream) {
      dom.textStream.textContent = '';
      dom.textStream.classList.remove('active');
    }
    if (dom.resultGrid) dom.resultGrid.innerHTML = '';
    state.currentResults = [];
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
      const batchSeed = state.generation.startTime || Date.now();
      const recipeSnapshot = getCurrentRecipeSnapshot(prompt);
      const enhanceFirstPrompt = state.selectedStyleChipIds.size > 0;
      for (let index = 0; index < count; index += 1) {
        if (state.generation.cancelRequested) break;

        const variationIndex = enhanceFirstPrompt ? index : index - 1;
        const variation = index === 0 && !enhanceFirstPrompt ? null : buildPromptVariation(Math.max(0, variationIndex), batchSeed);
        const actualPrompt = variation ? enhancePrompt(prompt, Math.max(0, variationIndex), batchSeed, variation) : prompt;
        const label = index === 0 && !enhanceFirstPrompt ? '原始提示词' : `风格增强 ${Math.max(1, index + (enhanceFirstPrompt ? 1 : 0))}`;
        const metadata = createGenerationMetadata(prompt, variation, recipeSnapshot);
        appendEvent('event', `生成第 ${index + 1}/${count} 张：${label}`);

        try {
          const result = await generateSingleImageWithRetry(actualPrompt, label, baseUrl, apiKey, model, null);
          state.generation.success += 1;
          try {
            await storeGeneratedImageResult(result, label, `img-${Date.now()}-${index}`, actualPrompt, 1, null, metadata);
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
        await storeGeneratedImageResult(result, '编辑结果', `img-${Date.now()}-single`, prompt, 2, refDataUrl, createGenerationMetadata(prompt, null, getCurrentRecipeSnapshot(prompt)));
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

  function enhancePrompt(basePrompt, index, batchSeed = 0, suppliedVariation = null) {
    const prompt = String(basePrompt || '').trim();
    const variation = suppliedVariation || buildPromptVariation(index, batchSeed);
    const styleChipLine = variation.styleChipLabels?.length
      ? `本次风格芯片：${variation.styleChipLabels.join(' / ')}。${variation.styleChipPrompt}`
      : '';
    return [
      `核心主题：${prompt}`,
      `增强版本：${index + 1}。编号只用于生成差异化，不要把编号、文字或水印画进图片。`,
      `人物原型：${variation.archetype}。`,
      `美貌与冲击力：${variation.beauty}。`,
      styleChipLine,
      `本次增强目标：在不改变核心主体和用户要求的前提下，生成与原始提示词版本以及其它增强版本明显不同的一张真实人物图；人物是绝对核心，每一张都要有倾国倾城、一眼万年的震撼感，优先体现真实长相、眼神、气质、宿命感和电影感。`,
      `真实人物约束：成年真实亚洲女性，脸部必须有真实人像质感，长相可信但极其惊艳，每一张脸都要有辨识度；皮肤可以漂亮、细腻、通透，但不要塑料皮肤、过度磨皮、AI 假脸、瓷娃娃感或网红模板脸。`,
      `展示风格原则：整体画面可以真实摄影、电影剧照、油画、厚涂插画、高级插画封面、动画电影感、东方奇幻感、科幻感或史诗感；不同展示风格都欢迎。`,
      `真实脸底线：不管画面多梦幻或多插画化，人物脸和眼神都要保留真人结构、可信光影、自然皮肤质感和有辨识度的五官；不要 Q 版、低幼卡通、极端二次元大眼、CG 娃娃、游戏建模脸、AI 假脸或网红模板脸。`,
      `背景原则：背景不必固定为野外，可以是自然、城市、室内、庄园、剧院、年代空间、花海、雪林、雨巷、科幻浮城或极简棚拍；风景越好看越好，但必须托住人物气质，不能抢走脸和眼神。`,
      `镜头与景别：${variation.framing}；${variation.lens}。`,
      `光线与氛围：${variation.lighting}；${variation.atmosphere}。`,
      `色彩方案：${variation.palette}。`,
      `场景调度：${variation.scene}。`,
      `背景风景：${variation.background}。`,
      `风格方向：${variation.style}。`,
      `细节要求：${variation.detail}。`,
      `质量约束：主体结构准确，脸、眼睛、手部、牙齿、身体比例自然；正式穿着、华丽礼服或幻想服饰都要符合人物气质；不要文字、水印、logo、多余肢体、畸形手指、网红模板脸、廉价影楼感、过度磨皮、低清晰度、重复人物。`,
    ].join('\n');
  }

  function buildPromptVariation(index, batchSeed = 0) {
    const styleChips = pickStyleChipsForVariation(index, batchSeed);
    const styleChipPrompt = styleChips.map((chip) => chip.prompt).join(' ');
    const styleAxis = pickPromptAxis('style', index, 9, 6, batchSeed);
    return {
      archetype: pickPromptAxis('archetype', index, 5, 8, batchSeed),
      beauty: pickPromptAxis('beauty', index, 7, 9, batchSeed),
      framing: pickPromptAxis('framing', index, 1, 0, batchSeed),
      lens: pickPromptAxis('lens', index, 3, 1, batchSeed),
      lighting: pickPromptAxis('lighting', index, 7, 2, batchSeed),
      atmosphere: pickPromptAxis('atmosphere', index, 9, 3, batchSeed),
      palette: pickPromptAxis('palette', index, 3, 4, batchSeed),
      scene: pickPromptAxis('scene', index, 7, 5, batchSeed),
      background: pickPromptAxis('background', index, 3, 8, batchSeed),
      style: [styleAxis, styleChipPrompt].filter(Boolean).join(' '),
      styleChipPrompt,
      styleChipIds: styleChips.map((chip) => chip.id),
      styleChipLabels: styleChips.map((chip) => chip.label),
      detail: pickPromptAxis('detail', index, 1, 7, batchSeed),
    };
  }

  function getCurrentRecipeSnapshot(prompt) {
    const selectedChips = getSelectedStyleChips();
    const metadata = getStyleChipMetadata(selectedChips);
    return {
      prompt: String(prompt || '').trim(),
      ...metadata,
      params: getImageParams(),
      capturedAt: new Date().toISOString(),
    };
  }

  function createGenerationMetadata(sourcePrompt, variation = null, recipeSnapshot = null) {
    const chips = variation?.styleChipIds?.length
      ? getStyleChipsByIds(variation.styleChipIds)
      : getSelectedStyleChips();
    return {
      sourcePrompt,
      ...getStyleChipMetadata(chips),
      recipeSnapshot,
    };
  }

  function pickPromptAxis(axisName, index, step, offset, batchSeed = 0) {
    const axis = PROMPT_VARIATION_AXES[axisName] || [];
    if (!axis.length) return '';
    const seedOffset = Math.abs(Math.floor(Number(batchSeed) || 0)) % axis.length;
    const cycleOffset = Math.floor((index + seedOffset) / axis.length);
    return axis[(index * step + offset + seedOffset + cycleOffset) % axis.length];
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

  async function storeGeneratedImageResult(result, label, imageName, prompt, mode, refDataUrl, metadata = {}) {
    try {
      const resolved = await resolveImageResult(result.imageSource, result.mediaAuth);
      await addResultCard(label, imageName, prompt, resolved.dataUrl, resolved.blob);
      await yieldToBrowser();
      await addToGallery(resolved.dataUrl, prompt, mode, refDataUrl, metadata);
      await yieldToBrowser();
      return resolved;
    } catch (error) {
      throw new Error(`${label} 图片已生成，但没能转存到浏览器本地图库：${error.message}`);
    }
  }

  async function addResultCard(label, imageName, prompt, dataUrl, blob) {
    const card = document.createElement('article');
    card.className = 'result-card';
    card.appendChild(createTextElement('div', 'label', label));
    const previewRecord = {
      id: `result-${Date.now()}-${Math.random().toString(36).slice(2)}`,
      dataUrl,
      prompt,
      mode: 1,
      createdAt: new Date().toISOString(),
    };
    const previewIndex = state.currentResults.length;
    state.currentResults.push(previewRecord);

    const image = document.createElement('img');
    image.src = dataUrl;
    image.alt = prompt || imageName;
    image.addEventListener('click', () => openPreviewList(state.currentResults, previewIndex));
    card.appendChild(image);

    const actions = document.createElement('div');
    actions.className = 'gallery-actions result-actions';
    const promptId = `result-prompt-${Date.now()}-${Math.random().toString(36).slice(2)}`;
    const toggleBtn = createTextElement('button', 'prompt-toggle-btn', '查看提示词');
    toggleBtn.type = 'button';
    toggleBtn.setAttribute('aria-expanded', 'false');
    toggleBtn.setAttribute('aria-controls', promptId);

    const downloadBtn = createTextElement('button', 'secondary', '下载');
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

    actions.append(toggleBtn, downloadBtn, copyBtn);

    const promptPanel = document.createElement('div');
    promptPanel.id = promptId;
    promptPanel.className = 'gallery-prompt-panel';
    promptPanel.hidden = true;

    const promptHeader = document.createElement('div');
    promptHeader.className = 'gallery-prompt-header';
    promptHeader.appendChild(createTextElement('span', '', '提示词'));

    const copyPromptBtn = createTextElement('button', 'copy-prompt', '复制提示词');
    copyPromptBtn.type = 'button';
    copyPromptBtn.addEventListener('click', () => copyText(prompt, '提示词已复制'));
    promptHeader.appendChild(copyPromptBtn);

    const promptEl = createTextElement('p', 'prompt-text', prompt || '这张图片没有保存提示词。');
    promptPanel.append(promptHeader, promptEl);

    toggleBtn.addEventListener('click', () => {
      promptPanel.hidden = !promptPanel.hidden;
      const expanded = !promptPanel.hidden;
      toggleBtn.textContent = expanded ? '收起提示词' : '查看提示词';
      toggleBtn.setAttribute('aria-expanded', String(expanded));
    });

    const info = document.createElement('div');
    info.className = 'result-info';
    info.append(actions, promptPanel);
    card.appendChild(info);
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
    state.streamTextBuffer += text;
    if (state.streamTextFlushPending) return;

    state.streamTextFlushPending = true;
    const flushVersion = state.streamTextVersion;
    const scheduleFrame = typeof window.requestAnimationFrame === 'function'
      ? window.requestAnimationFrame.bind(window)
      : (callback) => window.setTimeout(callback, 16);

    scheduleFrame(() => {
      if (flushVersion !== state.streamTextVersion) return;
      const pendingText = state.streamTextBuffer;
      state.streamTextBuffer = '';
      state.streamTextFlushPending = false;
      if (!pendingText || !dom.textStream) return;
      dom.textStream.textContent += pendingText;
      if (dom.statTextLen) dom.statTextLen.textContent = `文本: ${dom.textStream.textContent.length} 字`;
    });
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

  function yieldToBrowser() {
    return new Promise((resolve) => {
      if (typeof window.requestAnimationFrame === 'function') {
        window.requestAnimationFrame(() => resolve());
        return;
      }
      window.setTimeout(resolve, 16);
    });
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
      if (dom.networkStatusText) dom.networkStatusText.textContent = '检测中...';
      if (dom.networkStatusDot) dom.networkStatusDot.className = 'status-dot checking';
      if (dom.connectionStatus) dom.connectionStatus.textContent = '检测中';
      if (dom.networkStatusValue) dom.networkStatusValue.textContent = '检测中';
      return;
    }

    const online = state.network.isOnline;
    if (dom.networkStatusText) dom.networkStatusText.textContent = online ? '在线' : '离线';
    if (dom.networkStatusDot) dom.networkStatusDot.className = `status-dot ${online ? 'online' : 'offline'}`;
    if (dom.connectionStatus) {
      dom.connectionStatus.textContent = online ? '正常' : '断开';
      dom.connectionStatus.style.color = online ? 'var(--success)' : 'var(--danger)';
    }
    if (dom.networkStatusValue) dom.networkStatusValue.textContent = online ? '在线' : '离线';

    if (state.network.latency != null) {
      if (dom.networkLatency) dom.networkLatency.textContent = `${state.network.latency}ms`;
      if (dom.networkPing) {
        dom.networkPing.hidden = false;
        dom.networkPing.textContent = `${state.network.latency}ms`;
      }
    } else {
      if (dom.networkLatency) dom.networkLatency.textContent = '-';
      if (dom.networkPing) dom.networkPing.hidden = true;
    }
    if (dom.lastCheckTime) dom.lastCheckTime.textContent = state.network.lastCheck || '-';
  }

  function exportAllData() {
    const exportData = {
      version: '1.0',
      exportDate: new Date().toISOString(),
      gallery: state.gallery,
      apiConfigs: state.apiConfigs,
      activeApiId: state.activeApiId,
      promptHistory: state.promptHistory,
      promptRecipes: state.promptRecipes,
      selectedStyleChipIds: [...state.selectedStyleChipIds],
      imageParams: state.imageParams,
      backgroundImage: state.backgroundImage,
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

      state.gallery = data.gallery.map(normalizeGalleryRecord).filter(Boolean);
      state.apiConfigs = Array.isArray(data.apiConfigs) && data.apiConfigs.length
        ? removeRemovedDefaultConfigs(data.apiConfigs)
        : structuredCloneSafe(DEFAULT_CONFIGS);
      if (state.apiConfigs.length === 0) state.apiConfigs = structuredCloneSafe(DEFAULT_CONFIGS);
      const importedActiveId = data.activeApiId || state.apiConfigs[0]?.id || null;
      state.activeApiId = state.apiConfigs.some((config) => config.id === importedActiveId)
        ? importedActiveId
        : state.apiConfigs[0]?.id || null;
      state.promptHistory = Array.isArray(data.promptHistory) ? data.promptHistory.slice(0, MAX_PROMPT_HISTORY) : [];
      state.promptRecipes = Array.isArray(data.promptRecipes)
        ? data.promptRecipes.map(normalizePromptRecipe).filter(Boolean).slice(0, MAX_PROMPT_RECIPES)
        : [];
      state.selectedStyleChipIds = new Set(normalizeStyleChipIds(data.selectedStyleChipIds));
      state.imageParams = { ...state.imageParams, ...(data.imageParams || {}) };
      state.backgroundImage = typeof data.backgroundImage === 'string' ? data.backgroundImage : '';
      state.autoDownload = Boolean(data.autoDownload);

      await replaceGalleryRecords(state.gallery);
      saveApiConfigs();
      savePromptHistory();
      savePromptRecipes();
      saveStyleChipSelection();
      saveImageParams();
      try {
        localStorage.removeItem(STORAGE_KEYS.galleryLayout);
        localStorage.removeItem(STORAGE_KEYS.galleryFavoritesOnly);
      } catch {}
      saveBackgroundImage();
      saveAutoDownloadSetting();
      loadImageParams();
      applyBackgroundImage();
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
            sourcePrompt: item.sourcePrompt,
            mode: item.mode,
            time: item.time,
            params: item.params,
            styleChipIds: item.styleChipIds,
            styleChipLabels: item.styleChipLabels,
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
      state.promptRecipes = [];
      state.selectedStyleChipIds = new Set();
      state.refImages = [];
      state.backgroundImage = '';
      state.autoDownload = false;
      state.imageParams = { size: '1024x1024', quality: 'standard', style: 'natural' };
      saveApiConfigs();
      savePromptHistory();
      savePromptRecipes();
      saveStyleChipSelection();
      saveImageParams();
      saveAutoDownloadSetting();
      loadImageParams();
      applyBackgroundImage();
      dom.autoDownloadCheckbox.checked = false;
      renderAll();
      showStatus('done', '所有数据已清空');
    } catch (error) {
      showStatus('err', `清空失败：${error.message}`);
    }
  }

  async function clearAllImages() {
    if (state.generation.active) {
      showStatus('info', '正在生成中，请先取消或等待生成结束后再清除图片');
      return;
    }
    if (!state.gallery.length) {
      showStatus('info', '图库里没有可清除的图片');
      return;
    }

    const count = state.gallery.length;
    if (!confirm(`确定清除全部 ${count} 张图片吗？\n\n只会删除浏览器图库和当前结果区图片；API Key、提示词历史、图片参数和更换的背景都会保留。`)) return;

    try {
      state.gallery = [];
      state.currentResults = [];
      await replaceGalleryRecords([]);
      if (dom.resultGrid) dom.resultGrid.innerHTML = '';
      dom.resultArea?.classList.remove('active');
      if (dom.previewOverlay?.classList.contains('open')) closePreview();
      renderGalleryIfVisible();
      updateStorageInfo();
      showStatus('done', `已清除 ${count} 张图片，API 配置、提示词历史和背景已保留`);
    } catch (error) {
      showStatus('err', `清除图片失败：${error.message}`);
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
      state.preview.items = [];
      dom.previewImg.src = indexOrUrl;
      dom.previewNavPrev.hidden = true;
      dom.previewNavNext.hidden = true;
      dom.previewCounter.hidden = true;
    } else {
      openPreviewList(state.gallery, indexOrUrl, false);
      return;
    }
    dom.previewOverlay.classList.add('open');
  }

  function openPreviewList(items, index = 0, resetTransform = true) {
    const previewItems = Array.isArray(items)
      ? items.filter((item) => item?.dataUrl)
      : [];
    if (!previewItems.length) return;
    if (resetTransform) resetPreviewTransform();
    state.preview.urlMode = false;
    state.preview.items = previewItems;
    state.preview.index = Math.min(Math.max(0, index), previewItems.length - 1);
    showPreviewImage(state.preview.index);
    const canNavigate = previewItems.length > 1;
    dom.previewNavPrev.hidden = !canNavigate;
    dom.previewNavNext.hidden = !canNavigate;
    dom.previewCounter.hidden = !canNavigate;
    dom.previewOverlay.classList.add('open');
  }

  function closePreview() {
    dom.previewOverlay.classList.remove('open');
    dom.previewImg.src = '';
    state.preview.items = [];
  }

  function showPreviewImage(index) {
    const record = state.preview.items[index];
    if (!record) return;
    state.preview.index = index;
    dom.previewImg.src = record.dataUrl;
    dom.previewCounter.textContent = `${index + 1} / ${state.preview.items.length}`;
    updatePreviewNavigation();
  }

  function updatePreviewNavigation() {
    const lastIndex = state.preview.items.length - 1;
    dom.previewNavPrev.classList.toggle('disabled', state.preview.index <= 0);
    dom.previewNavNext.classList.toggle('disabled', state.preview.index >= lastIndex);
  }

  function prevImage() {
    if (state.preview.index > 0) {
      resetPreviewTransform();
      showPreviewImage(state.preview.index - 1);
    }
  }

  function nextImage() {
    if (state.preview.index < state.preview.items.length - 1) {
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
    if (document.body.classList.contains('bg-only') && event.key === 'Escape') {
      document.body.classList.remove('bg-only');
      return;
    }
    if (!dom.previewOverlay.classList.contains('open')) return;
    if (event.key === 'Escape') closePreview();
    if (!state.preview.urlMode && event.key === 'ArrowLeft') prevImage();
    if (!state.preview.urlMode && event.key === 'ArrowRight') nextImage();
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
