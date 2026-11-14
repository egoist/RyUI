// The words of the site's own pages, in each language. (No React here: the
// Markdown downloads, built by the Vite config, take the site's description.)

import type { Locale } from "./locales.ts"

const en = {
  siteTitle: "Asuka — Native widgets in Go, on phones, desktops and the web",
  siteDescription:
    "Build apps in Go with each platform's own widgets, on iOS, Android, macOS, Windows, Linux and the web. Asuka renders with each platform's own widgets (UIKit, Material 3, AppKit, Win32, GTK 4 and libadwaita, the DOM), updated by fine-grained signals, with live reload as you save and release builds for every platform.",
  nav: { docs: "Docs", examples: "Examples", releases: "Releases", menu: "Navigation menu", license: "MIT License" },
  language: { label: "Language", auto: "Auto" },
  search: {
    button: "Search docs…",
    title: "Search the docs",
    description: "Find a page or a section of the docs.",
    placeholder: "Search the docs…",
    failed: "The search index could not be loaded.",
    loading: "Loading…",
    empty: (query: string) => `No results for “${query}”`,
  },
  docs: {
    overview: "Overview",
    introduction: "Introduction",
    more: "More",
    menu: "Menu",
    documentation: "Documentation",
    previous: "Previous episode",
    next: "Next episode",
    edit: "Edit this page on GitHub",
    copyCode: "Copy code",
    sync: "Sync",
  },
  notFound: {
    label: "Error 404 / Pattern unknown",
    title: "This page does not exist",
    text: "It may have moved. The docs, or their search, will have what you are looking for.",
    docs: "Read the docs",
    home: "Home",
    pageTitle: "Page not found · Asuka",
  },
}

export type Messages = typeof en

const zh: Messages = {
  siteTitle: "Asuka — 用 Go 写原生控件应用，手机、桌面、Web 全覆盖",
  siteDescription:
    "用 Go 开发应用，在 iOS、Android、macOS、Windows、Linux 和 Web 上使用各平台自己的控件（UIKit、Material 3、AppKit、Win32、GTK 4 与 libadwaita、DOM）。界面采用细粒度更新，保存即重载，每个平台都能直接打出发布构建。",
  nav: { docs: "文档", examples: "示例", releases: "版本发布", menu: "导航菜单", license: "MIT 许可证" },
  language: { label: "语言", auto: "自动" },
  search: {
    button: "搜索文档…",
    title: "搜索文档",
    description: "查找文档中的页面或章节。",
    placeholder: "搜索文档…",
    failed: "搜索索引加载失败。",
    loading: "加载中…",
    empty: (query: string) => `没有找到与“${query}”相关的内容`,
  },
  docs: {
    overview: "概览",
    introduction: "简介",
    more: "更多",
    menu: "目录",
    documentation: "文档",
    previous: "上一话",
    next: "下一话",
    edit: "在 GitHub 上编辑此页",
    copyCode: "复制代码",
    sync: "同步率",
  },
  notFound: {
    label: "Error 404 / 图形不明",
    title: "页面不存在",
    text: "它可能已经换了地方。去文档里看看，或者搜索一下，应该能找到你要的内容。",
    docs: "阅读文档",
    home: "首页",
    pageTitle: "页面不存在 · Asuka",
  },
}

const ja: Messages = {
  siteTitle: "Asuka — Go で書く、ネイティブ UI のアプリ。スマートフォンにも、デスクトップにも、Web にも",
  siteDescription:
    "Go で書いたアプリを、iOS、Android、macOS、Windows、Linux、Web で、各プラットフォーム純正のウィジェット（UIKit、Material 3、AppKit、Win32、GTK 4 と libadwaita、DOM）で動かせます。画面はきめ細かなシグナルで必要な部分だけが更新され、保存するたびにライブでリロードされます。リリースビルドもすべてのプラットフォーム向けに作れます。",
  nav: { docs: "ドキュメント", examples: "サンプル", releases: "リリース", menu: "ナビゲーションメニュー", license: "MIT ライセンス" },
  language: { label: "言語", auto: "自動" },
  search: {
    button: "ドキュメントを検索…",
    title: "ドキュメントを検索",
    description: "ドキュメントのページやセクションを探します。",
    placeholder: "ドキュメントを検索…",
    failed: "検索インデックスを読み込めませんでした。",
    loading: "読み込み中…",
    empty: (query: string) => `「${query}」に一致する結果はありません`,
  },
  docs: {
    overview: "概要",
    introduction: "はじめに",
    more: "その他",
    menu: "目次",
    documentation: "ドキュメント",
    previous: "前回",
    next: "次回",
    edit: "GitHub でこのページを編集",
    copyCode: "コードをコピー",
    sync: "シンクロ率",
  },
  notFound: {
    label: "Error 404 / パターン不明",
    title: "このページは存在しません",
    text: "移動した可能性があります。ドキュメントか、その検索で探しているものが見つかるはずです。",
    docs: "ドキュメントを読む",
    home: "ホーム",
    pageTitle: "ページが見つかりません · Asuka",
  },
}

export const messages: Record<Locale, Messages> = { en, zh, ja }
