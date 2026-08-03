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
                    // between pages automatically where they overlap. Only shared vendor
                    // libraries are grouped by hand here, so they get one stable,
                    // cacheable chunk instead of being duplicated across page chunks.
                    manualChunks: (id) => {
                        if (id.includes('node_modules')) {
                            // MUI packages
                            if (id.includes('@mui/material') || id.includes('@mui/system') || id.includes('@mui/utils')) {
                                return 'mui-vendor';
                            }
                            if (id.includes('@mui/icons-material')) {
                                return 'mui-icons-vendor';
                            }
                            // Recharts + d3
                            if (id.includes('recharts') || id.includes('d3-') || id.includes('victory-')) {
                                return 'recharts-vendor';
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