import react from '@vitejs/plugin-react-swc';
import { defineConfig } from 'vite';
import { visualizer } from 'rollup-plugin-visualizer';
import path from 'path';

// Web-only Vite configuration
// For Wails builds, use vite.config.wails.ts instead
export default defineConfig(({ mode }) => {
    // Check if we should use mock data
    const useMock = process.env.USE_MOCK === 'true'
    console.log("use mock", useMock)

    return {
        plugins: [
            react(),
            // Bundle analyzer - only in analyze mode for development
            // Run with: pnpm build -- --mode analyze
            mode === 'analyze' && visualizer({
                open: true,
                gzipSize: true,
                brotliSize: true,
                filename: 'dist/stats.html',
            }),
        ].filter(Boolean),
        define: {
            // Make USE_MOCK available to the app
            'import.meta.env.VITE_USE_MOCK': JSON.stringify(useMock ? 'true' : 'false'),
        },
        resolve: {
            alias: {
                // Web mode: always use mock bindings
                '@/bindings': '/src/bindings-web',
                '@': path.resolve(__dirname, './src'),
            }
        },
        server: {
            proxy: useMock ? {} : {
                '/api/': {
                    target: 'http://localhost:12580',
                    changeOrigin: true,
                    secure: false,
                },
                '/tingly/': {
                    target: 'http://localhost:12580',
                    changeOrigin: true,
                    secure: false,
                }
            },
            port: 3000
        },
        // Memory optimization for build process
        optimizeDeps: {
            // Pre-bundle large dependencies to reduce build memory
            include: [
                'react',
                'react-dom',
                '@mui/material',
                '@mui/icons-material',
            ],
        },
        build: {
            rollupOptions: {
                output: {
                    // Routes are lazy-loaded (see App.tsx), so Rollup already splits each
                    // page into its own chunk at the import() boundary, and shares code
                    // between pages automatically where they overlap.
                    //
                    // Only MUI is forced into its own vendor chunk here: it's imported
                    // eagerly by Layout/App (not just lazy pages), so it belongs in the
                    // always-loaded set and benefits from a stable, cacheable chunk name.
                    //
                    // recharts/d3 deliberately have NO manual rule. They're only used by
                    // two lazy pages (Dashboard, UserUsage) and are never imported eagerly
                    // — but forcing them into a named "recharts-vendor" chunk previously
                    // made the bundler treat that chunk as always-needed and preload it
                    // from index.html on every page load (~850KB, unused outside those two
                    // routes). Leaving them unnamed lets Rollup fold them into a
                    // dynamic-import-only shared chunk that's fetched solely when one of
                    // those two pages is actually visited. Verify with `pnpm build` +
                    // check dist/index.html's <link rel="modulepreload"> list before
                    // re-adding a manual grouping for either of these.
                    manualChunks: (id) => {
                        if (id.includes('node_modules')) {
                            // MUI packages
                            if (id.includes('@mui/material') || id.includes('@mui/system') || id.includes('@mui/utils')) {
                                return 'mui-vendor';
                            }
                            if (id.includes('@mui/icons-material')) {
                                return 'mui-icons-vendor';
                            }
                        }
                        return undefined;
                    },
                },
                maxParallelFileOps: 4,
            },
            chunkSizeWarningLimit: 500,
            // Disable sourcemap in production to reduce memory and output size
            sourcemap: mode !== 'production',
            // Use SWC for minification (via @vitejs/plugin-react-swc)
            minify: 'swc',
        },
    }
})