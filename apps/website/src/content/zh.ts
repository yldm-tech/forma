import type { SiteContent } from "./schema";

export const zh: SiteContent = {
  common: {
    skipToContent: "跳到正文",
    homeLabel: "Forma 首页",
    nav: {
      label: "主导航",
      product: "产品",
      linkSurveys: "链接问卷",
      websiteSurveys: "网站问卷",
      inAppSurveys: "应用内问卷",
      integrations: "集成",
      developers: "开发者",
      sdk: "SDK 参考",
      selfHosting: "私有化部署",
      github: "GitHub",
      openMenu: "打开菜单",
      closeMenu: "关闭菜单",
      language: "语言",
    },
    cta: {
      primary: { closed: "私有化部署 Forma", open: "开始使用" },
      signIn: "登录",
    },
    beta: "Beta",
    copy: { button: "复制", done: "已复制" },
    ctaBand: {
      title: "从一个问题开始",
      body: {
        closed:
          "用 Docker Compose 几分钟就能部署好 Forma；如果你的团队已经在用 forma.yldm.ai，直接登录即可。",
        open: "注册账号，或自行部署。",
      },
    },
    footer: {
      label: "页脚",
      product: "产品",
      developers: "开发者",
      project: "项目",
      reportIssue: "反馈问题",
      security: "安全",
      copyright: "© 2026 YLDM",
    },
    localeHint: { text: "本页面提供简体中文版本", dismiss: "关闭" },
    illustrations: {
      surveyTitle: "客户反馈",
      ratingQuestion: "你如何评价这次体验？",
      npsQuestion: "你有多大可能向朋友推荐我们？",
      npsLow: "不太可能",
      npsHigh: "非常可能",
      textPlaceholder: "再多说几句…",
      submit: "提交",
      popoverQuestion: "你找到想要的内容了吗？",
      popoverYes: "找到了",
      popoverNo: "还没有",
      summaryTitle: "你最看重哪一点？",
      summaryOptions: ["易用性", "速度", "集成", "支持"],
      workflowTrigger: "触发条件",
      workflowTriggerValue: "回答提交完成",
      workflowFilter: "筛选",
      workflowFilterValue: "结束页：感谢页",
      workflowAction: "动作",
      workflowActionValue: "发送邮件",
      segmentLabel: "分群",
      segmentValue: "套餐为试用版",
      eventLabel: "操作",
      eventValue: "导出了报表",
      newResponse: "新回答",
    },
  },
  pages: {
    home: {
      meta: {
        title: "Forma：链接问卷、网站问卷与应用内问卷",
        description:
          "用无代码编辑器搭建问卷，精准定向答题人群，查看结果并通过 Webhook 推送；私有化部署还可以同步到 Slack、Notion、Google Sheets。",
      },
      hero: {
        eyebrow: "问卷与用户反馈",
        title: "问对的人，把反馈变成行动。",
        sub: "Forma 是一个问卷平台，支持链接问卷、网站问卷和应用内问卷。用无代码编辑器搭建问卷，精确决定由谁来回答，查看结果，再把数据送到团队已经在用的工具里。",
        note: {
          closed:
            "你可以使用 forma.yldm.ai 上的托管服务（目前仅限受邀用户），也可以把 Forma 部署在自己的服务器上。",
          open: "在 forma.yldm.ai 注册账号，或把 Forma 部署在自己的服务器上。",
        },
      },
      ways: {
        title: "三种提问方式",
        cards: [
          {
            title: "链接问卷",
            body: "通过链接分享、嵌入网页，或发送个人专属链接和一次性链接。需要确保回答可信时，可以加上 PIN 码或邮箱验证。",
          },
          {
            title: "网站问卷",
            body: "在访客点击、滚动、停留或准备离开页面时弹出问卷。装好代码片段之后，触发条件都在 Forma 里设置，不用再改网站代码。",
          },
          {
            title: "应用内问卷",
            body: "识别已登录用户、附加用户属性，在合适的时机只问你真正关心的那群人。",
          },
        ],
      },
      build: {
        title: "搭建真正需要的问卷",
        body: "从 50 多个模板或空白问卷开始。18 种题型可以和条件逻辑、隐藏字段、变量与答案引用自由组合，样式也能调整成你的品牌风格。",
        checklist: [
          "填空、单选与多选、图片选择、评分、NPS、CSAT、CES、滑块、排序、矩阵、日期与时间、地址、联系方式、文件上传、同意确认、说明页，以及通过 Cal.com 预约会议",
          "一份问卷，多种语言",
          "支持保存未完成的回答、配额、定时发布和回答数量上限",
        ],
      },
      understand: {
        title: "在问卷里直接读懂结果",
        body: "每份问卷都有独立的汇总视图和回答视图。可以按天、周或月查看回答数、完成率、NPS 和 CSAT 的趋势，筛选回答、打标签，导出为 CSV 或 Excel，也能通过公开链接分享结果。",
      },
      act: {
        title: "让每条回答都有下文",
        body: "用 Webhook 把回答推送到任意地址；私有化部署还可以同步到 Slack、Notion、Airtable 或 Google Sheets。Workflows（Beta）会在回答提交完成后自动发邮件，还可以只针对你选定的结束页触发。",
        link: "查看集成 →",
      },
      ai: {
        title: "AI 按需开启",
        body: "用一段话描述需求，就能生成可编辑的问卷草稿；一键把问卷翻译成已启用的语言；还能生成示例回答，提前看看分析效果。AI 默认关闭，只有组织的所有者或管理者才能开启。",
      },
      developers: {
        title: "为接入而设计",
        body: "加一段 script 标签，就能显示网站问卷和应用内问卷。通过 v3 REST API 管理问卷、回答和工作流，或者用你自己的 Forma 账号把 AI Agent 接到 MCP 服务器上。",
        link: "开发者概览 →",
      },
      hosting: {
        title: "用我们的服务器，或你自己的",
        hostedTitle: "托管版",
        hosted: [
          "forma.yldm.ai",
          "服务运行和升级由我们负责",
          { closed: "目前仅限受邀用户", open: "使用邮箱注册" },
        ],
        selfHostedTitle: "私有化部署",
        selfHosted: [
          "Docker Compose、Ubuntu 一键安装脚本或 Helm Chart",
          "数据库和对象存储都在你手里",
          "支持 Google、Microsoft Entra ID、OIDC、SAML 单点登录",
        ],
        footer: "源代码在 GitHub 上公开。",
      },
    },
    formBuilder: {
      meta: {
        title: "链接问卷与表单 · Forma",
        description: "18 种题型、条件逻辑、隐藏字段与多语言。可通过链接分享、嵌入网页或发送一次性链接。",
      },
      title: "链接问卷与表单",
      sub: "问卷只需搭建一次，就能发到任何能放链接的地方：邮件、聊天工具、二维码，或者嵌进你自己的网页。",
      share: {
        title: "按你的方式分发",
        items: [
          "自定义链接地址",
          "通过 iframe 嵌入",
          "为每位联系人生成专属链接",
          "一次性链接",
          "通过 URL 参数追踪来源",
          "用 URL 预填答案",
        ],
      },
      control: {
        title: "控制谁来回答",
        body: "可以为问卷设置 PIN 码、要求先验证邮箱再作答，还能限制回答数量或定时开放；私有化部署还可以开启 reCAPTCHA 防刷。",
      },
      logic: {
        title: "逻辑与数据",
        body: "用条件逻辑设计分支，用隐藏字段和变量携带上下文，用答案引用复述前面的回答，未完成的回答也会保存下来。",
      },
      languages: {
        title: "一份问卷，多种语言",
        body: "不用复制多份问卷，只需给同一份问卷添加翻译。答题者可以自己切换语言，也可以通过 `?lang=` 直接打开对应语言版本，所有回答都汇总在同一份结果里。",
      },
      templates: {
        title: "模板",
        body: "50 多个模板，提供简体中文、繁体中文、英文和日文版本。",
      },
    },
    websiteSurvey: {
      meta: {
        title: "网站问卷 · Forma",
        description:
          "在点击、浏览、滚动、停留或离开意图时向访客展示问卷。装好一段脚本后，无需改代码即可配置。",
      },
      title: "网站问卷",
      sub: "在合适的时机向访客提问，不用让他们离开当前页面。",
      install: {
        title: "一次安装",
        body: "在网站里粘贴一段 script 标签，或者通过 Google Tag Manager 添加。其余配置都在 Forma 里完成。",
      },
      triggers: {
        title: "无代码触发条件",
        items: ["点击元素（按文字或 CSS 选择器）", "页面浏览", "离开意图", "滚动到 50%", "页面停留时间"],
        body: "每个触发条件都可以限定 URL：完全匹配、包含、开头为、结尾为，或正则表达式。",
      },
      considerate: {
        title: "不打扰访客",
        body: "可以设置同一位访客多久能再次被提问、问卷之间的冷却期，也可以只向一定比例的访客展示问卷。",
      },
      style: {
        title: "与网站风格一致",
        body: "工作区主题、单个问卷的样式覆盖，以及自定义 CSS。",
      },
    },
    inAppSurvey: {
      meta: {
        title: "应用内问卷 · Forma",
        description: "识别用户、补充属性、创建分群，并在产品内的特定操作发生时触发问卷。",
      },
      title: "应用内问卷，面向已登录用户",
      sub: "识别用户、补充用户属性，只向那些对当下决策真正有参考价值的人提问。",
      identify: {
        title: "识别并补充用户信息",
        body: "通过 SDK 从你的应用传入用户 ID 和属性，联系人及其属性都保存在 Forma 中。",
      },
      segments: {
        title: "按分群定向",
        body: "基于属性创建用户分群，让问卷只在产品内特定操作发生时展示给这个分群。",
      },
      codeActions: {
        title: "代码事件",
        body: "除了无代码触发条件，还可以在代码里上报任意事件，作为问卷的触发条件。",
      },
      language: {
        title: "语言",
        body: "通过 SDK 设置用户语言。如果多语言问卷没有该语言的翻译，Forma 就不会向该用户展示这份问卷。",
      },
    },
    integrations: {
      meta: {
        title: "集成与工作流 · Forma",
        description:
          "用 Webhook 推送回答，私有化部署还可以同步到 Slack、Notion、Airtable、Google Sheets，并用 Workflows（Beta）在回答完成后自动发邮件。",
      },
      title: "集成与工作流",
      sub: "回答只有被看到才有价值。把它们送到团队工作的地方。",
      native: {
        title: "原生集成",
        body: "私有化部署配置好对应服务的 OAuth 应用后，即可连接账号，选择新回答要发送到哪里。",
      },
      webhooks: {
        title: "Webhook",
        body: "回答创建、更新或完成时，向你的接口发送 HTTP 请求。每个 Webhook 都会显示投递是否成功，保留最近的投递记录，并可重新发送其中任意一条。",
      },
      automation: {
        title: "自动化工具",
        body: "Zapier、Make、n8n 和 Activepieces 通过 Webhook API 订阅回答。",
      },
      workflows: {
        title: "工作流（Workflows）",
        body: "触发条件：回答提交完成。筛选：只针对你选定的结束页。动作：发送邮件。每次运行都会记录状态和各步骤日志。",
        badgeNote: "Beta：触发条件和动作会陆续增加。",
      },
    },
    developers: {
      meta: {
        title: "开发者 · Forma",
        description: "用于产品内问卷的 JavaScript 代码片段、v3 REST API，以及面向 AI Agent 的 MCP 服务器。",
      },
      title: "开发者",
      sub: "一段 script 标签负责产品内问卷，其余一切交给 REST API。",
      snippet: {
        title: "JavaScript 代码片段",
        body: "从你的 Forma 实例加载 `/js/forma.umd.js`，并在页面上渲染问卷。加载完成前的调用会先排队。",
        link: "SDK 参考 →",
      },
      api: {
        title: "REST API v3",
        body: "使用 API 密钥管理问卷和工作流。回答和 Webhook 另有管理接口，使用同一个密钥。",
        link: "REST API 参考 →",
      },
      mcp: {
        title: "MCP 服务器",
        body: "把 MCP 客户端指向 `https://forma.yldm.ai/api/mcp`，用你自己的 Forma 账号通过 OAuth 登录并授权所需权限，Agent 就能查看、创建和编辑问卷与工作流。",
        link: "连接客户端 →",
      },
      webhooks: {
        title: "Webhook",
        body: "回答创建、更新和完成时，实时推送到你自己的接口。",
        link: "查看集成 →",
      },
      reference: {
        title: "API 参考",
        lead: "以下路径都相对于你的 Forma 实例：`https://forma.yldm.ai`，私有化部署时则是你自己的域名。请求和响应均为 JSON。",
        rest: {
          title: "REST API",
          facts: [
            {
              term: "基础路径",
              body: "`/api/v3`。错误以 RFC 9457 问题文档（`application/problem+json`）返回。",
            },
            {
              term: "认证",
              body: "在 `X-Api-Key` 请求头中发送密钥，或使用 `Authorization: Bearer fbk_…`。不要把密钥放进 URL。",
            },
            {
              term: "API 密钥",
              body: "由组织所有者或管理者在应用中打开“组织设置 → API 密钥 → 添加 API 密钥”，按工作区逐一授予读取、写入或管理权限，然后复制密钥。密钥只显示一次。创建密钥的成员离开组织、被移出组织、注销账号或角色被调整为低于管理者时，该密钥会被永久停用。可以在“API 密钥”页面查看哪些密钥已停用及原因。",
            },
            {
              term: "OpenAPI",
              body: "`/api/v3/openapi.json` 以 OpenAPI 3.1 格式描述了 API 密钥可以调用的全部操作，可直接导入 API 客户端或代码生成工具。获取它不需要密钥。",
            },
          ],
          resourcesTitle: "主要资源",
          resources: [
            "`/api/v3/me`：密钥所属的组织，以及它可以使用的工作区和在每个工作区中的权限。需要查找 `workspaceId` 时从这里开始。",
            "`/api/v3/surveys`：列出、创建、校验、读取、更新、归档、恢复和删除问卷。",
            "`/api/v3/workflows`：创建和编辑工作流，启用、停用或测试，并查看运行记录。",
            "`/api/v3/action-classes` 和 `/api/v3/contact-attribute-keys`：应用内问卷所针对的动作和属性，只读。",
            "`/api/v2/management/responses` 和 `/api/v2/management/webhooks`：回答和 Webhook 订阅，使用同一个密钥。",
          ],
        },
        mcp: {
          title: "MCP 服务器",
          facts: [
            {
              term: "端点",
              body: "`/api/mcp`，使用 Streamable HTTP，例如 `https://forma.yldm.ai/api/mcp`。",
            },
            {
              term: "OAuth",
              body: "支持 OAuth 的客户端会自动找到登录入口：用你的 Forma 账号登录，并批准它请求的权限范围。",
            },
            {
              term: "API 密钥",
              body: "不支持 OAuth 的客户端可以像调用 REST API 一样，在请求头中发送 API 密钥。只读密钥可以查看；有写入或管理权限的密钥还可以创建和编辑。",
            },
          ],
        },
      },
    },
    sdk: {
      meta: {
        title: "Forma JavaScript SDK 参考",
        description: "用一个 script 标签安装 Forma SDK，识别用户、设置属性、追踪操作、监听事件并调试集成。",
      },
      title: "JavaScript SDK 参考",
      sub: "一个 script 标签即可展示网站问卷和应用内问卷，其余操作都是对 `window.forma` 的调用。",
      install: {
        title: "安装",
        body: "在 Forma 的“集成 → Web 和移动 SDK”中复制代码片段（已填好你的工作区 ID 和实例 URL），粘贴到每个页面的 `<head>` 中。使用 Google Tag Manager 时，将其粘贴到触发条件为“所有页面”的自定义 HTML 代码中。",
        frameworks:
          'React 和 Vue：粘贴到 `index.html`。Next.js：用 `next/script` 并设置 `strategy="beforeInteractive"` 渲染同一段 JavaScript，这样 `window.forma` 会在组件的 effect 运行前就已存在：App Router 放在根 `app/layout.tsx` 中，Pages Router 直接放在 `pages/_document.tsx` 中。无需路由 hook。SDK 尚未发布到 npm，移动端 SDK 和 WordPress 插件也暂未提供。',
        queue:
          '脚本加载完成前，`window.forma` 是一个占位对象：调用会先进入队列而不会报错，SDK 加载并完成初始化后按顺序执行。排队中的调用没有返回值，请不要 await。SDK 加载后，占位对象会把调用转发给 SDK，因此加载前保存的引用仍然有效。使用 TypeScript 时，声明一次全局变量，逐个写出你调用的方法名，不要使用索引签名（开启 `noUncheckedIndexedAccess` 时，通过索引签名取到的方法可能为 undefined）：`interface Window { forma: Record<"setUserId" | "track" | "logout", (...args: unknown[]) => unknown> }`。',
      },
      reference: {
        title: "方法",
        lead: "所有方法都通过 `window.forma` 调用。代码片段会替你调用 `setup({ workspaceId, appUrl })`；只有不使用代码片段、直接加载 `/js/forma.umd.js` 时才需要自己调用，并且要最先调用。",
        identify: {
          title: "识别用户",
          facts: [
            {
              term: "setUserId(userId, { userHash })",
              body: "将访客识别为该 ID 对应的联系人（首次使用时创建），使分群定向和“只显示一次”规则按人生效。ID 最长 255 个字符；设置不同的 ID 时会先清除上一位用户的状态。`userHash` 用于验证该 ID。",
            },
            {
              term: "setAttributes(attributes)、setAttribute(key, value)",
              body: "设置用于分群的联系人属性，需先调用 `setUserId`。字符串、数字或 `Date` 决定新属性的类型；之后的值必须是同一类型，否则服务端不会保存。",
            },
            {
              term: "身份验证",
              body: "工作区 ID 是公开的，因此 SDK 不会写入在 Forma 中标记为受保护的属性，邮箱也只为已验证的用户写入。要验证用户，请在“集成 → Web 和移动 SDK”中生成身份验证密钥，在你的服务器上用它计算用户 ID 的 HMAC-SHA256（十六进制），并作为 `userHash` 传入。开启“要求身份验证”后，没有有效哈希的识别请求会被拒绝。",
            },
            {
              term: "setLanguage(language)",
              body: "以该语言展示问卷：语言代码（如 `de`）或你配置的别名。匿名访客同样适用。",
            },
            {
              term: "logout()",
              body: "在当前浏览器中忘记该用户。请在用户退出登录时调用，避免同一设备上的下一个人被当作上一个人。",
            },
          ],
        },
        actions: {
          title: "操作与单页应用",
          facts: [
            {
              term: "track(code, { hiddenFields })",
              body: "触发该键对应的代码操作（在 Forma 的“操作”中设置），可能会弹出问卷。`hiddenFields` 用于填充问卷的隐藏字段并随回答保存；该参数已弃用，将在后续版本中移除。",
            },
            {
              term: "页面浏览与路由",
              body: "SDK 会修补 `history.pushState` 和 `replaceState`，并监听 `popstate` 和 `hashchange`，因此客户端导航本身就算一次页面浏览。对于两者都不使用的路由，可调用 `registerRouteChange()` 手动重新检查 URL。",
            },
            {
              term: "setEmbeddedData(data)、clearEmbeddedData(key?)",
              body: "为之后的回答附加数据而不触发任何操作；`{ key: null }` 会删除该键，只有问卷声明为接收的嵌入数据字段会被保留。同步执行，不发送网络请求。",
            },
          ],
        },
        events: {
          title: "事件",
          body: "`on(event, handler)` 返回一个取消订阅的函数，`off(event, handler)` 作用相同。同样的事件也会以 `forma` 键推送到 `window.dataLayer`，供 Google Tag Manager 使用。事件数据只包含 ID，不包含回答内容。",
          facts: [
            { term: "forma_setup_successful", body: "`{ workspaceId }`：初始化完成，SDK 已就绪。" },
            {
              term: "forma_setup_failed",
              body: "`{ workspaceId, code, message }`：初始化未完成。`code` 为 `missing_field`、`invalid_config`、`network_error` 或 `error_state`。",
            },
            { term: "forma_action_tracked", body: "`{ action }`：通过 `track()` 或在页面上追踪到一次操作。" },
            { term: "forma_survey_shown", body: "`{ surveyId }`：问卷已显示在屏幕上。" },
            {
              term: "forma_response_submitted",
              body: "`{ surveyId, responseId, finished }`：第一次作答时（`finished: false`）触发，完成的回答保存后再次触发。",
            },
            { term: "forma_survey_closed", body: "`{ surveyId }`：问卷被关闭或已完成。" },
          ],
        },
        debugging: {
          title: "调试与安全",
          facts: [
            {
              term: "?formaDebug=true",
              body: "加到页面 URL 上后，SDK 会在控制台以 `🧱 Forma` 前缀记录每一步，重新获取工作区状态而不使用缓存，并忽略已保存的错误状态。当前用户看不到的每个问卷都会连同原因一起记录，例如 `Survey <id> left out: workspace cooldown 7d, last survey shown 2d ago`；被追踪的动作触发了却无法显示的问卷也会记录。",
            },
            {
              term: "setNonce(nonce)",
              body: "若样式使用基于 nonce 的内容安全策略，请在问卷渲染前传入 nonce（不含 `nonce-`）。并在 `script-src` 和 `connect-src` 中允许你的 Forma 实例。",
            },
            {
              term: "缓存",
              body: "工作区状态有缓存，因此问卷和操作的修改可能需要几分钟才会到达已加载 SDK 的页面。调试模式会跳过缓存。",
            },
          ],
        },
      },
    },
    selfHosting: {
      meta: {
        title: "私有化部署 Forma · Docker / Helm",
        description: "使用 Docker Compose、Ubuntu 一键脚本或 Helm Chart 部署 Forma，附硬件要求与部署方式。",
      },
      // U+200B marks where the heading may wrap: headings in Chinese only wrap at punctuation and spaces (styles.css).
      title: "在自己的基础设施上\u200b运行 Forma",
      sub: "一个容器里的 Go 程序，加上 PostgreSQL、Valkey 和 SpiceDB。选择适合你团队的部署方式。",
      optionsTitle: "选择部署方式",
      docker: {
        title: "Docker Compose",
        body: "下载 Compose 文件，创建 `.env`，执行 `docker compose up`。",
        link: "在 GitHub 查看 Compose 文件 →",
      },
      oneClick: {
        title: "一键安装（Ubuntu）",
        body: "一个 Shell 脚本即可完成 Forma 的部署，并配置好 Traefik 与 HTTPS 证书。",
        link: "在 GitHub 查看安装脚本 →",
      },
      kubernetes: {
        title: "Kubernetes",
        body: "官方 Helm Chart，可按需对接托管的 PostgreSQL、Redis 和兼容 S3 的对象存储。",
        link: "在 GitHub 查看 Helm Chart →",
      },
      requirements: {
        title: "硬件要求",
        body: "评估用途：2 vCPU、4 GB 内存、20 GB SSD。单机低负载：4 vCPU、8 GB 内存、50 GB SSD。必须使用 HTTPS。",
      },
      signIn: {
        title: "登录方式",
        body: "邮箱密码登录并支持双因素认证，另可配置 Google、Microsoft Entra ID、OpenID Connect 和 SAML 单点登录。",
      },
      ai: {
        title: "可选的 AI 功能",
        body: "AI 功能需要你自行配置模型接口；不配置的话，Forma 照常运行，只是不带 AI 功能。",
      },
      source: {
        title: "源代码",
        body: "完整源代码在 GitHub 上。",
        link: "在 GitHub 上查看 →",
      },
    },
    security: {
      meta: {
        title: "安全 · Forma",
        description: "如何报告安全漏洞，以及 Forma 提供的账号安全功能。",
      },
      title: "安全",
      report: {
        title: "报告安全漏洞",
        body: "请发送邮件至 security@yldm.ai，不要公开提交 issue。我们没有漏洞赏金计划；如果你希望，我们会公开致谢。",
      },
      account: {
        title: "账号安全",
        body: "双因素认证、支持团队和工作区级角色的组织管理、审计日志；私有化部署还可以使用单点登录。",
      },
      policyLink: "在 GitHub 上查看完整政策 →",
    },
  },
  notFound: {
    meta: { title: "页面不存在 · Forma", description: "这个页面可能已经移动了。" },
    title: "页面不存在",
    body: "这个页面可能已经移动了。如果你在找 Forma 应用，它现在位于 /app。",
    home: "首页",
  },
};
