/* eslint-env node */
module.exports = {
  root: true,
  env: { browser: true, es2022: true },
  extends: [
    'eslint:recommended',
    'plugin:vue/vue3-recommended',
    'plugin:@typescript-eslint/recommended',
    // Must stay last (LYCM-117): it switches off every eslint rule that
    // prettier already decides, so the two tools stop undoing each other.
    // Without it `bun run format` and `eslint --fix` disagree about the same
    // markup and neither converges, which is how 697 unfixable warnings piled
    // up. Formatting is prettier's; eslint keeps correctness.
    'prettier',
  ],
  parser: 'vue-eslint-parser',
  parserOptions: {
    parser: '@typescript-eslint/parser',
    ecmaVersion: 'latest',
    sourceType: 'module',
  },
  plugins: ['@typescript-eslint'],
  rules: {
    'vue/multi-word-component-names': 'off',
  },
}
