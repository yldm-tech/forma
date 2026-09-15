# Forma Browser JS Library

[![npm package](https://img.shields.io/npm/v/@forma/js?style=flat-square)](https://www.npmjs.com/package/@forma/js)
[![MIT License](https://img.shields.io/badge/License-MIT-red.svg?style=flat-square)](https://opensource.org/licenses/MIT)

Please see [Forma Docs](https://forma.ylam.ai/docs).
Specifically, [Quickstart/Implementation details](https://forma.ylam.ai/docs/getting-started/quickstart-in-app-survey).

## What is Forma

Forma is your go-to solution for in-product micro-surveys that will supercharge your product experience! 🚀 For more information please check out [forma.ylam.ai](https://forma.ylam.ai).

## How to use this library

1. Install the Forma package inside your project using npm:

```bash
npm install -s @forma/js
```

2. Import Forma and initialize the widget in your main component (e.g., App.tsx or App.js):

```javascript
import forma from "@forma/js";

if (typeof window !== "undefined") {
  forma.setup({
    workspaceId: "your-workspace-id",
    appUrl: "https://app.forma.ylam.ai",
  });
}
```

Replace your-environment-id with your actual environment ID. You can find your environment ID in the **Setup Checklist** in the Forma settings.

For more detailed guides for different frameworks, check out our [Framework Guides](https://forma.ylam.ai/docs/getting-started/framework-guides).
