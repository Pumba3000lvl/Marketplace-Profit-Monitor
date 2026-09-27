declare module 'replace-in-file-webpack-plugin' {
  import { Compiler, Plugin } from 'webpack';

  interface ReplaceRule {
    search: string | RegExp;
    replace: string | ((match: string) => string);
  }

  interface ReplaceOption {
    dir?: string;
    files?: string[];
    test?: RegExp | RegExp[];
    rules: ReplaceRule[];
  }

  class ReplaceInFilePlugin extends Plugin {
    constructor(options?: ReplaceOption[]);
    options: ReplaceOption[];
    apply(compiler: Compiler): void;
  }

  export = ReplaceInFilePlugin;
}

declare module 'webpack-livereload-plugin' {
  import { ServerOptions } from 'https';
  import { Compilation, Compiler, Plugin, Stats } from 'webpack';

  interface Options extends Pick<ServerOptions, 'cert' | 'key' | 'pfx'> {
    protocol?: string;
    port?: number;
    hostname?: string;
    appendScriptTag?: boolean;
    ignore?: RegExp | RegExp[] | null;
    delay?: number;
    useSourceHash?: boolean;
  }

  class LiveReloadPlugin extends Plugin {
    readonly isRunning: boolean;
    constructor(options?: Options);
    apply(compiler: Compiler): void;
    start(watching: unknown, callback: () => void): void;
    done(stats: Stats): void;
    failed(): void;
    autoloadJs(): string;
    scriptTag(source: string): string;
    applyCompilation(compilation: Compilation): void;
  }

  export = LiveReloadPlugin;
}
