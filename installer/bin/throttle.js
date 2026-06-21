#!/usr/bin/env node
"use strict";
const { main } = require("../src/cli");
Promise.resolve(main(process.argv.slice(2)))
  .then((code) => process.exit(code || 0))
  .catch((e) => {
    console.error(String((e && e.message) || e));
    process.exit(1);
  });
