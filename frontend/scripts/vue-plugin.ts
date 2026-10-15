/**
 * Bun plugin compiling Vue single-file components (SFCs).
 *
 * Bun 1.4 does not natively compile `.vue` (it copies SFCs as static assets verbatim),
 * and the official `bun-plugin-vue` is an empty shell. This compiles with Vue's own
 * `@vue/compiler-sfc`, keeping the Bun build chain without pulling in a Vite runtime dep.
 */
import type { BunPlugin } from "bun";
import { compileScript, compileTemplate, parse } from "@vue/compiler-sfc";

export function vuePlugin(): BunPlugin {
  return {
    name: "paperly-vue-sfc",
    setup(build) {
      build.onLoad({ filter: /\.vue$/ }, async (args) => {
        const source = await Bun.file(args.path).text();
        const { descriptor, errors } = parse(source, { filename: args.path });
        if (errors.length > 0) {
          throw new Error(`[vue] ${args.path}: ${errors[0]?.message ?? "parse error"}`);
        }

        const id = args.path;
        const script = compileScript(descriptor, { id });

        // With no template, return the script result directly.
        if (!descriptor.template) {
          return { loader: "ts", contents: script.content };
        }

        const tpl = compileTemplate({
          source: descriptor.template.content,
          filename: args.path,
          id,
          compilerOptions: { bindingMetadata: script.bindings },
        });
        if (tpl.errors.length > 0) {
          throw new Error(`[vue] ${args.path}: ${String(tpl.errors[0])}`);
        }

        // script.content looks like `export default /*@__PURE__*/_defineComponent({...})`.
        // Split out the default export, inject render, then re-export — avoids duplicate declarations.
        const scriptBody = script.content.replace(/export\s+default\s+/, "const __sfc__ = ");
        const code = [
          scriptBody,
          tpl.code.replace(/export\s+function\s+render/, "function render"),
          "__sfc__.render = render;",
          "export default __sfc__;",
        ].join("\n");

        return { loader: "ts", contents: code };
      });
    },
  };
}
