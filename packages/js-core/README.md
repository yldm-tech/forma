# Forma Browser JS Library

[![npm package](https://img.shields.io/npm/v/@forma/js?style=flat-square)](https://www.npmjs.com/package/@forma/js)
[![MIT License](https://img.shields.io/badge/License-MIT-red.svg?style=flat-square)](https://opensource.org/licenses/MIT)

## What is Forma

Forma is your go-to solution for in-product micro-surveys that will supercharge your product experience! 🚀 For more information please check out [forma.yldm.ai](https://forma.yldm.ai).

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
    appUrl: "https://forma.yldm.ai",
  });
}
```

Replace your-environment-id with your actual environment ID. You can find your environment ID in the **Setup Checklist** in the Forma settings.
