import type { SiteContent } from "./schema";

export const ja: SiteContent = {
  common: {
    skipToContent: "本文へスキップ",
    homeLabel: "Forma ホーム",
    nav: {
      label: "メインナビゲーション",
      product: "製品",
      linkSurveys: "リンクアンケート",
      websiteSurveys: "Web サイトアンケート",
      inAppSurveys: "アプリ内アンケート",
      integrations: "連携",
      developers: "開発者向け",
      selfHosting: "セルフホスト",
      docs: "ドキュメント",
      github: "GitHub",
      openMenu: "メニューを開く",
      closeMenu: "メニューを閉じる",
      language: "言語",
    },
    cta: {
      primary: { closed: "セルフホストする", open: "はじめる" },
      signIn: "ログイン",
      docs: "ドキュメント",
    },
    beta: "ベータ版",
    copy: { button: "コピー", done: "コピーしました" },
    ctaBand: {
      title: "まずは 1 問から",
      body: {
        closed:
          "Docker Compose なら数分で Forma をセルフホストできます。チームですでに forma.yldm.ai を使っている場合は、そのままログインしてください。",
        open: "アカウントを作成するか、セルフホストできます。",
      },
    },
    footer: {
      label: "フッター",
      product: "製品",
      developers: "開発者向け",
      project: "プロジェクト",
      apiReference: "API リファレンス",
      mcp: "MCP",
      reportIssue: "問題を報告",
      security: "セキュリティ",
      copyright: "© 2026 YLDM",
    },
    localeHint: { text: "このページは日本語でもご覧いただけます", dismiss: "閉じる" },
    illustrations: {
      surveyTitle: "お客様の声",
      ratingQuestion: "ご利用の満足度を教えてください",
      npsQuestion: "友人や同僚に薦める可能性はどのくらいですか？",
      npsLow: "可能性は低い",
      npsHigh: "可能性は高い",
      textPlaceholder: "詳しく教えてください…",
      submit: "送信",
      popoverQuestion: "お探しの情報は見つかりましたか？",
      popoverYes: "はい",
      popoverNo: "まだです",
      summaryTitle: "最も重視している点は？",
      summaryOptions: ["使いやすさ", "速さ", "連携", "サポート"],
      workflowTrigger: "トリガー",
      workflowTriggerValue: "回答の完了",
      workflowFilter: "フィルター",
      workflowFilterValue: "終了画面：サンクスページ",
      workflowAction: "アクション",
      workflowActionValue: "メール送信",
      segmentLabel: "セグメント",
      segmentValue: "プランがトライアル",
      eventLabel: "アクション",
      eventValue: "レポートをエクスポート",
    },
  },
  pages: {
    home: {
      meta: {
        title: "Forma｜リンク・Web サイト・アプリ内アンケート",
        description:
          "ノーコードでアンケートを作成し、回答者を絞り込み、結果を Slack・Notion・Google スプレッドシート・Webhook へ連携。ホスティング版とセルフホストに対応。",
      },
      hero: {
        eyebrow: "アンケートとフィードバック",
        title: "聞くべき人に聞き、その声を次の一手に。",
        sub: "Forma は、リンク・Web サイト・アプリ内で使えるアンケートプラットフォームです。ノーコードのエディタで作成し、回答してほしい相手を絞り込み、結果を確認して、チームが普段使っているツールへそのまま届けられます。",
        note: {
          closed:
            "forma.yldm.ai のホスティング版（現在は招待制）を使うことも、自社のサーバーで Forma を動かすこともできます。",
          open: "forma.yldm.ai でアカウントを作成するか、自社のサーバーで Forma を動かせます。",
        },
      },
      ways: {
        title: "3 つの聞き方",
        cards: [
          {
            title: "リンクアンケート",
            body: "リンクで共有したり、Web ページに埋め込んだり、個別リンクや使い捨てリンクを送ったりできます。回答の信頼性が必要な場合は、PIN やメール認証を追加できます。",
          },
          {
            title: "Web サイトアンケート",
            body: "訪問者がクリックしたとき、スクロールしたとき、一定時間とどまったとき、離脱しようとしたときにアンケートを表示します。スニペットを設置したあとは、表示条件を Forma 側で設定でき、サイトのコードを触る必要はありません。",
          },
          {
            title: "アプリ内アンケート",
            body: "ログイン中のユーザーを識別して属性をひも付け、聞きたいセグメントだけに、ちょうどよいタイミングで質問できます。",
          },
        ],
      },
      build: {
        title: "必要なアンケートを、そのまま形に",
        body: "50 以上のテンプレート、または白紙の状態から始められます。17 種類の設問に、条件分岐、隠しフィールド、変数、回答の引用を組み合わせ、デザインもブランドに合わせて調整できます。",
        checklist: [
          "自由記述、単一・複数選択、画像選択、評価、NPS、CSAT、CES、ランキング、マトリクス、日付、住所、連絡先、ファイルアップロード、同意、説明文、Cal.com での日程予約",
          "1 つのアンケートで多言語に対応",
          "途中保存、クォータ、公開スケジュール、回答数の上限",
        ],
      },
      understand: {
        title: "集めたその場で結果を読み解く",
        body: "アンケートごとに、集計ビューと回答ビューが用意されています。回答の絞り込みやタグ付け、CSV・Excel へのエクスポートのほか、公開リンクで結果を共有することもできます。",
      },
      act: {
        title: "回答を、次のアクションへ",
        body: "回答を Slack、Notion、Airtable、Google スプレッドシートへ送ったり、Webhook で任意のエンドポイントへ送信したりできます。Workflows（ベータ版）では、回答が完了したときにメールを送信でき、特定の終了画面に到達した回答だけを対象にすることもできます。",
        link: "連携機能を見る →",
      },
      ai: {
        title: "AI は、必要なときだけ",
        body: "作りたいアンケートを文章で伝えると、編集できる下書きが作られます。有効にした言語への翻訳や、分析画面を試すためのサンプル回答の生成もできます。AI は初期状態では無効で、組織のオーナーまたはマネージャーが有効にするまで使われません。",
      },
      developers: {
        title: "組み込みやすい設計",
        body: "script タグを 1 つ追加するだけで、Web サイトやアプリ内でアンケートを表示できます。v3 REST API でアンケート・回答・ワークフローを管理したり、自分の Forma アカウントで AI エージェントを MCP サーバーに接続したりできます。",
        link: "開発者向け情報 →",
      },
      hosting: {
        title: "私たちのサーバーでも、自社のサーバーでも",
        hostedTitle: "ホスティング版",
        hosted: [
          "forma.yldm.ai",
          "サービスの運用とアップデートは私たちが担当",
          { closed: "現在は招待制", open: "メールアドレスで登録" },
        ],
        selfHostedTitle: "セルフホスト",
        selfHosted: [
          "Docker Compose、Ubuntu 向けワンクリックスクリプト、Helm チャート",
          "データベースもオブジェクトストレージも自社で管理",
          "Google、Microsoft Entra ID、OIDC、SAML によるシングルサインオン",
        ],
        footer: "ソースコードは GitHub で公開しています。",
      },
    },
    formBuilder: {
      meta: {
        title: "リンクアンケート・フォーム作成｜Forma",
        description:
          "17 種類の設問、条件分岐、隠しフィールド、多言語に対応。リンク共有、ページへの埋め込み、使い捨てリンクで回答を集められます。",
      },
      title: "リンクアンケートとフォーム",
      sub: "一度作ったアンケートを、リンクを置けるあらゆる場所へ。メール、チャット、QR コード、自社ページへの埋め込みにも対応します。",
      share: {
        title: "好きな方法で届ける",
        items: [
          "URL のカスタマイズ",
          "iframe での埋め込み",
          "連絡先ごとの個別リンク",
          "使い捨てリンク",
          "URL パラメータによる流入元の記録",
          "URL からの回答の事前入力",
        ],
      },
      control: {
        title: "回答者をコントロール",
        body: "PIN による保護、回答前のメール認証、reCAPTCHA によるスパム対策に加え、回答数の上限や公開期間も設定できます。",
      },
      logic: {
        title: "ロジックとデータ",
        body: "条件分岐で流れを変え、隠しフィールドや変数で文脈を引き継ぎ、前の回答を引用して表示できます。途中までの回答も保存されます。",
      },
      languages: {
        title: "1 つのアンケートで多言語に",
        body: "アンケートを複製せず、1 つのアンケートに翻訳を追加します。回答者は言語を選ぶか `?lang=` 付きのリンクから開くだけで、すべての回答が同じ結果に集まります。",
      },
      templates: {
        title: "テンプレート",
        body: "50 以上のテンプレートを、日本語・英語・中国語で用意しています。",
      },
    },
    websiteSurvey: {
      meta: {
        title: "Web サイトアンケート｜Forma",
        description:
          "クリック、ページビュー、スクロール、滞在時間、離脱の兆候をきっかけに表示。script タグを設置したあとはノーコードで設定できます。",
      },
      title: "Web サイト上のアンケート",
      sub: "ページを離れてもらうことなく、ちょうどよいタイミングで訪問者に質問できます。",
      install: {
        title: "設置は一度だけ",
        body: "script タグを 1 つ貼り付けるか、Google タグマネージャーで追加するだけ。それ以降の設定はすべて Forma で行えます。",
      },
      triggers: {
        title: "ノーコードの表示条件",
        items: [
          "要素のクリック（テキストまたは CSS セレクタで指定）",
          "ページビュー",
          "離脱の兆候",
          "50% スクロール",
          "ページ滞在時間",
        ],
        body: "それぞれの条件は、URL の完全一致・部分一致・前方一致・後方一致・正規表現で対象ページを絞り込めます。",
      },
      considerate: {
        title: "訪問者への配慮",
        body: "同じ訪問者に再度表示するまでの間隔や、アンケート同士のクールダウン期間を設定できます。訪問者の一部の割合だけに表示することも可能です。",
      },
      style: {
        title: "サイトになじむデザイン",
        body: "ワークスペース単位のテーマ、アンケートごとのスタイル上書き、カスタム CSS に対応しています。",
      },
    },
    inAppSurvey: {
      meta: {
        title: "アプリ内アンケート｜Forma",
        description:
          "ユーザーの識別、属性の付与、セグメント作成、プロダクト内の操作をきっかけにしたアンケート表示。",
      },
      title: "ログインユーザー向けのアプリ内アンケート",
      sub: "ユーザーを識別して属性を加え、いま判断したいことに関係する人だけに質問できます。",
      identify: {
        title: "識別と属性の付与",
        body: "SDK を通じて、アプリからユーザー ID と属性を送信します。連絡先とその属性は Forma に保存されます。",
      },
      segments: {
        title: "セグメントで絞り込む",
        body: "属性からセグメントを作り、プロダクト内の特定の操作をきっかけに、そのセグメントだけにアンケートを表示します。",
      },
      codeActions: {
        title: "コードアクション",
        body: "ノーコードの条件に加えて、コードから任意のイベントを送信し、表示のきっかけとして使えます。",
      },
      language: {
        title: "言語",
        body: "SDK でユーザーの言語を設定します。その言語の翻訳がないアンケートは、そのユーザーには表示されません。",
      },
    },
    integrations: {
      meta: {
        title: "連携とワークフロー｜Forma",
        description:
          "Slack・Notion・Airtable・Google スプレッドシート・Webhook へ回答を送信。Workflows（ベータ版）で回答完了時にメールを送れます。",
      },
      title: "連携とワークフロー",
      sub: "回答は、誰かの目に届いてはじめて意味を持ちます。チームが働く場所へ届けましょう。",
      native: {
        title: "ネイティブ連携",
        body: "アカウントを接続し、新しい回答の送信先を選ぶだけです。",
      },
      webhooks: {
        title: "Webhook",
        body: "回答の作成・更新・完了時に、指定したエンドポイントへ HTTP リクエストを送信します。",
      },
      automation: {
        title: "自動化ツール",
        body: "Zapier、Make、n8n、Activepieces との連携手順をガイドで紹介しています。",
        link: "連携ガイドを見る →",
      },
      workflows: {
        title: "ワークフロー",
        body: "トリガー：回答の完了。フィルター：指定した終了画面のみ。アクション：メール送信。実行ごとにステータスと各ステップのログが記録されます。",
        badgeNote: "ベータ版：トリガーとアクションは今後追加される予定です。",
      },
    },
    developers: {
      meta: {
        title: "開発者向け｜Forma",
        description:
          "プロダクト内アンケート用の JavaScript スニペット、v3 REST API、AI エージェント向け MCP サーバー。",
      },
      title: "開発者向け",
      sub: "プロダクト内のアンケートは script タグで、それ以外は REST API で。",
      snippet: {
        title: "JavaScript スニペット",
        body: "Forma インスタンスから `/js/forma.umd.cjs` を読み込み、ページ上にアンケートを表示します。",
      },
      api: {
        title: "REST API v3",
        body: "API キーで、アンケート・回答・ワークフロー・タグを管理できます。API は OpenAPI で定義されています。",
        link: "API リファレンス →",
      },
      mcp: {
        title: "MCP サーバー",
        body: "MCP クライアントの接続先を `https://forma.yldm.ai/api/mcp` に設定し、自分の Forma アカウントで OAuth ログインして権限を承認すると、エージェントがアンケートやワークフローの一覧表示・作成・編集を行えます。",
        link: "MCP ガイド →",
      },
      webhooks: {
        title: "Webhook",
        body: "回答の作成・更新・完了のたびに、自社のエンドポイントで受け取れます。",
        link: "連携機能を見る →",
      },
    },
    selfHosting: {
      meta: {
        title: "Forma をセルフホスト｜Docker・Helm",
        description:
          "Docker Compose、Ubuntu 用ワンクリックスクリプト、Helm チャートで Forma を導入。要件とセットアップ手順。",
      },
      title: "自社のインフラで Forma を動かす",
      sub: "コンテナで動く 1 つの Go バイナリと、PostgreSQL・Valkey・SpiceDB。チームに合った方法を選べます。",
      optionsTitle: "導入方法を選ぶ",
      docker: {
        title: "Docker Compose",
        body: "Compose ファイルをダウンロードし、`.env` を作成して `docker compose up` を実行します。",
        link: "Docker ガイド →",
      },
      oneClick: {
        title: "ワンクリック（Ubuntu）",
        body: "シェルスクリプト 1 本で、Traefik と HTTPS 証明書を含めて Forma をセットアップします。",
        link: "ワンクリックガイド →",
      },
      kubernetes: {
        title: "Kubernetes",
        body: "公式 Helm チャート。必要に応じて、マネージドの PostgreSQL、Redis、S3 互換ストレージを利用できます。",
        link: "Kubernetes ガイド →",
      },
      requirements: {
        title: "要件",
        body: "評価用：2 vCPU、メモリ 4 GB、SSD 20 GB。単一サーバーで低負荷の運用：4 vCPU、8 GB、SSD 50 GB。HTTPS が必要です。",
      },
      signIn: {
        title: "サインイン方法",
        body: "メールアドレスとパスワード（2 要素認証対応）に加え、Google、Microsoft Entra ID、OpenID Connect、SAML によるシングルサインオンを設定できます。",
      },
      ai: {
        title: "オプションの AI",
        body: "AI 機能を使うには、モデルのエンドポイントを設定する必要があります。設定しなければ、Forma は AI なしで動作します。",
      },
      source: {
        title: "ソースコード",
        body: "ソースコードはすべて GitHub で公開しています。",
        link: "GitHub で見る →",
      },
    },
    security: {
      meta: {
        title: "セキュリティ｜Forma",
        description: "脆弱性の報告方法と、Forma のアカウントセキュリティ機能。",
      },
      title: "セキュリティ",
      report: {
        title: "脆弱性の報告",
        body: "公開の issue ではなく、security@yldm.ai までメールでご連絡ください。バグバウンティは実施していませんが、ご希望があれば公開でクレジットを記載します。",
      },
      account: {
        title: "アカウントのセキュリティ",
        body: "2 要素認証、チームとワークスペース単位のロールを備えた組織管理、監査ログ。セルフホスト環境ではシングルサインオンも利用できます。",
      },
      policyLink: "GitHub でポリシー全文を読む →",
    },
  },
  notFound: {
    meta: { title: "ページが見つかりません｜Forma", description: "ページが移動した可能性があります。" },
    title: "ページが見つかりません",
    body: "ページが移動した可能性があります。Forma のアプリをお探しの場合は、/app からアクセスしてください。",
    home: "ホーム",
  },
};
