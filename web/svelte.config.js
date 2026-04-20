import adapter from '@sveltejs/adapter-static';

/** @type {import('@sveltejs/kit').Config} */
const config = {
  kit: {
    adapter: adapter({
      pages: '../internal/dashboard/static',
      assets: '../internal/dashboard/static',
      fallback: 'index.html',
      precompress: false
    }),
    paths: {
      base: ''
    }
  }
};

export default config;
