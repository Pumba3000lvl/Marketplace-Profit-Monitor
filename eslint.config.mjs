import { defineConfig } from 'eslint/config';
import typescriptParser from '@typescript-eslint/parser';
import grafanaConfig from '@grafana/eslint-config/flat.js';

export default defineConfig([
  ...grafanaConfig,
  {
    files: ['src/**/*.{ts,tsx}'],
    languageOptions: {
      parser: typescriptParser,
      parserOptions: {
        project: './tsconfig.json',
      },
    },
  },
]);
