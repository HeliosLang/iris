import { dirname, join } from "node:path"
import { defineConfig } from "vite"
import makeReactPlugin from "@vitejs/plugin-react"
import { viteStaticCopy as makeStaticCopy } from "vite-plugin-static-copy"

const repoRoot = join(dirname(process.argv[1]), "../../../")
const assetsDir = join(repoRoot, "assets")
const dstDir = join(repoRoot, "tmp/frontend")

export default defineConfig({
    root: join(repoRoot, "src/frontend"),
    build: {
        outDir: dstDir,
        emptyOutDir: true,
        minify: true
    },
    publicDir: assetsDir,
    plugins: [
        makeReactPlugin({
            babel: {
                plugins: [
                    [
                        "babel-plugin-styled-components",
                        {
                            ssr: false,
                            pure: true,
                            displayName: true,
                            fileName: true
                        }
                    ]
                ]
            }
        }),
        makeStaticCopy({
            targets: [
                {
                    src: join(assetsDir, "favicon.ico"),
                    dest: dstDir
                }
            ]
        })
    ]
})
