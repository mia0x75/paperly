import { createApp } from "vue";
import { createPinia } from "pinia";
import App from "./App.vue";
import router from "./router";
import { initThemeMode } from "./theme/mode";

// Apply the theme mode first (sets <html class="dark">), then mount, to avoid first-frame flash.
initThemeMode();

const app = createApp(App);
app.use(createPinia());
app.use(router);
app.mount("#app");
