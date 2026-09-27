/// <reference path="../types/webpack-plugins.d.ts" />

import CopyWebpackPlugin from 'copy-webpack-plugin';
import ForkTsCheckerWebpackPlugin from 'fork-ts-checker-webpack-plugin';
import path from 'path';
import { readFileSync } from 'fs';
import ReplaceInFileWebpackPlugin from 'replace-in-file-webpack-plugin';
import TerserPlugin from 'terser-webpack-plugin';
import { SubresourceIntegrityPlugin } from 'webpack-subresource-integrity';
import webpack, { type Configuration } from 'webpack';
import LiveReloadPlugin from 'webpack-livereload-plugin';
import VirtualModulesPlugin from 'webpack-virtual-modules';

interface PluginConfig {
  id: string;
  info: { logos: { small: string; large: string } };
}

interface PackageConfig {
  version: string;
}

const plugin: PluginConfig = JSON.parse(readFileSync(path.resolve(process.cwd(), 'src/plugin.json'), 'utf8'));
const packageJson: PackageConfig = JSON.parse(readFileSync(path.resolve(process.cwd(), 'package.json'), 'utf8'));
const outputDirectory = path.resolve(process.cwd(), 'dist');
const pluginId = plugin.id as string;

const logoPaths = [...new Set([plugin.info.logos.small, plugin.info.logos.large])];
const copyPatterns = [
  { from: 'plugin.json', to: '.' },
  ...logoPaths.map((logo: string) => ({ from: logo, to: logo })),
];

export default (env: Record<string, string | boolean>): Configuration => ({
  cache: {
    type: 'filesystem',
    buildDependencies: {
      config: [path.resolve(process.cwd(), '.config/webpack/webpack.config.ts')],
    },
  },
  context: path.resolve(process.cwd(), 'src'),
  devtool: env.production ? 'source-map' : 'eval-source-map',
  entry: { module: './module.ts' },
  externals: {
    'amd-module': 'module',
    '@grafana/data': 'amd @grafana/data',
    '@grafana/runtime': 'amd @grafana/runtime',
    '@grafana/schema': 'amd @grafana/schema',
    '@grafana/ui': 'amd @grafana/ui',
    react: 'amd react',
    'react-dom': 'amd react-dom',
  },
  mode: env.production ? 'production' : 'development',
  module: {
    rules: [
      {
        test: /src\/(?:.*\/)?module\.tsx?$/,
        use: [{ loader: 'imports-loader', options: { imports: 'side-effects grafana-public-path' } }],
      },
      {
        test: /\.[tj]sx?$/,
        exclude: /node_modules/,
        use: {
          loader: 'swc-loader',
          options: {
            jsc: {
              target: 'es2015',
              parser: { syntax: 'typescript', tsx: true },
            },
          },
        },
      },
      {
        test: /\.(png|jpe?g|gif|svg)$/,
        type: 'asset/resource',
      },
    ],
  },
  optimization: {
    minimize: Boolean(env.production),
    minimizer: [
      new TerserPlugin({
        extractComments: false,
        terserOptions: { format: { comments: false } },
      }),
    ],
  },
  output: {
    clean: { keep: /^gpx_marketplace_profit_/ },
    filename: '[name].js',
    library: { type: 'amd' },
    path: outputDirectory,
    publicPath: `public/plugins/${pluginId}/`,
    uniqueName: pluginId,
    crossOriginLoading: 'anonymous',
  },
  plugins: [
    new VirtualModulesPlugin({
      'node_modules/grafana-public-path.js': `
        import amdMetaModule from 'amd-module';
        __webpack_public_path__ = amdMetaModule && amdMetaModule.uri
          ? amdMetaModule.uri.slice(0, amdMetaModule.uri.lastIndexOf('/') + 1)
          : 'public/plugins/${pluginId}/';
      `,
    }),
    new webpack.BannerPlugin({
      banner: `/* plugin: ${pluginId}@${packageJson.version} */`,
      raw: true,
      entryOnly: true,
    }),
    new CopyWebpackPlugin({ patterns: copyPatterns }),
    new ReplaceInFileWebpackPlugin([
      {
        dir: outputDirectory,
        test: [/(^|\/)plugin\.json$/],
        rules: [
          { search: /\%VERSION\%/g, replace: packageJson.version },
          { search: /\%TODAY\%/g, replace: new Date().toISOString().slice(0, 10) },
        ],
      },
    ]),
    new SubresourceIntegrityPlugin({ hashFuncNames: ['sha256'] }),
    ...(env.development
      ? [
          new LiveReloadPlugin(),
          new ForkTsCheckerWebpackPlugin({
            async: true,
            typescript: { configFile: path.resolve(process.cwd(), 'tsconfig.json') },
          }),
        ]
      : []),
  ],
  resolve: {
    extensions: ['.js', '.jsx', '.ts', '.tsx'],
    modules: [path.resolve(process.cwd(), 'src'), 'node_modules'],
  },
});
