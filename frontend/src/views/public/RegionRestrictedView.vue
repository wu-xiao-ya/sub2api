<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { useAppStore } from '@/stores/app'

const route = useRoute()
const appStore = useAppStore()

const siteName = computed(() => appStore.siteName || 'PetraFlux Labs')
const siteLogo = computed(() => appStore.siteLogo || '/logo.svg')
const contactInfo = computed(() => appStore.contactInfo.trim())
const detectedCountry = computed(() => {
  const value = Array.isArray(route.query.country) ? route.query.country[0] : route.query.country
  return typeof value === 'string' && value.trim() ? value.trim().toUpperCase() : 'CN'
})
</script>

<template>
  <main class="restriction-page">
    <img
      src="/region-restricted-bg.png"
      alt=""
      aria-hidden="true"
      class="restriction-background"
    />
    <div class="restriction-overlay"></div>

    <header class="restriction-header">
      <div class="restriction-brand">
        <img :src="siteLogo" alt="" class="restriction-logo" />
        <span>{{ siteName }}</span>
      </div>
      <span class="restriction-status">HTTP 403</span>
    </header>

    <section class="restriction-content" aria-labelledby="restriction-title">
      <div class="restriction-kicker">
        <span class="restriction-kicker-icon" aria-hidden="true">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor">
            <path d="M12 3 4.5 6v5.5c0 4.8 3.1 7.8 7.5 9.5 4.4-1.7 7.5-4.7 7.5-9.5V6L12 3Z" />
            <path d="M9.5 12h5M12 9.5v5" />
          </svg>
        </span>
        区域服务调整
      </div>

      <p class="restriction-code">ACCESS RESTRICTED / REGION {{ detectedCountry }}</p>
      <h1 id="restriction-title">暂不向中国大陆地区提供服务</h1>
      <p class="restriction-summary">
        根据平台区域合规、网络稳定性及风险控制策略，自
        <strong>2026年8月20日</strong>
        起，本站控制台及 API 服务将不再向中国大陆地区开放。
      </p>

      <div class="restriction-divider"></div>

      <div class="restriction-details">
        <p>
          系统判断当前连接来源于中国大陆地区，因此本次访问已被拒绝。账户数据、API
          密钥及余额信息将按照平台现有规则保留和处理。
        </p>
        <p lang="en">
          This service is not currently available in mainland China. Account data, API
          keys, and balances will continue to be handled under the platform's existing
          policies.
        </p>
      </div>

      <div v-if="contactInfo" class="restriction-contact">
        <span>账户或余额问题请联系官方支持</span>
        <strong>{{ contactInfo }}</strong>
      </div>

      <footer class="restriction-footer">
        <span>{{ siteName }}</span>
        <span>REGION RESTRICTED · REQUEST DENIED</span>
      </footer>
    </section>
  </main>
</template>

<style scoped>
.restriction-page {
  position: relative;
  min-height: 100svh;
  overflow: hidden;
  background: #0a0f18;
  color: #f8fafc;
  font-family:
    system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC",
    "Microsoft YaHei", sans-serif;
}

.restriction-background,
.restriction-overlay {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
}

.restriction-background {
  object-fit: cover;
  opacity: 0.72;
}

.restriction-overlay {
  background: rgba(3, 7, 18, 0.7);
}

.restriction-header {
  position: relative;
  z-index: 2;
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: min(1180px, calc(100% - 48px));
  margin: 0 auto;
  padding: 28px 0;
}

.restriction-brand {
  display: flex;
  align-items: center;
  min-width: 0;
  gap: 12px;
  color: #e2e8f0;
  font-size: 14px;
  font-weight: 650;
}

.restriction-logo {
  width: 34px;
  height: 34px;
  flex: none;
  object-fit: contain;
}

.restriction-status {
  color: #94a3b8;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12px;
}

.restriction-content {
  position: relative;
  z-index: 2;
  width: min(780px, calc(100% - 48px));
  margin: clamp(70px, 13vh, 150px) auto 60px;
}

.restriction-kicker {
  display: flex;
  align-items: center;
  gap: 9px;
  color: #5eead4;
  font-size: 13px;
  font-weight: 650;
}

.restriction-kicker-icon {
  display: inline-flex;
  width: 25px;
  height: 25px;
  align-items: center;
  justify-content: center;
  border: 1px solid rgba(94, 234, 212, 0.42);
  border-radius: 50%;
}

.restriction-kicker-icon svg {
  width: 15px;
  height: 15px;
  stroke-width: 1.6;
}

.restriction-code {
  margin: 26px 0 12px;
  color: #94a3b8;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12px;
}

h1 {
  max-width: 760px;
  margin: 0;
  color: #ffffff;
  font-size: clamp(36px, 6.2vw, 68px);
  font-weight: 760;
  line-height: 1.12;
  letter-spacing: 0;
  text-wrap: balance;
}

.restriction-summary {
  max-width: 690px;
  margin: 26px 0 0;
  color: #cbd5e1;
  font-size: clamp(16px, 2vw, 20px);
  line-height: 1.85;
}

.restriction-summary strong {
  color: #ffffff;
  font-weight: 700;
}

.restriction-divider {
  width: 100%;
  height: 1px;
  margin: 38px 0 30px;
  background: rgba(148, 163, 184, 0.28);
}

.restriction-details {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 32px;
}

.restriction-details p {
  margin: 0;
  color: #aebbc9;
  font-size: 14px;
  line-height: 1.9;
}

.restriction-contact {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 20px;
  margin-top: 34px;
  padding-top: 22px;
  border-top: 1px solid rgba(148, 163, 184, 0.16);
  color: #94a3b8;
  font-size: 13px;
}

.restriction-contact strong {
  max-width: 58%;
  color: #e2e8f0;
  font-weight: 600;
  overflow-wrap: anywhere;
  text-align: right;
}

.restriction-footer {
  display: flex;
  justify-content: space-between;
  gap: 24px;
  margin-top: 52px;
  color: #64748b;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 11px;
}

@media (max-width: 720px) {
  .restriction-header {
    width: min(100% - 32px, 1180px);
    padding-top: 20px;
  }

  .restriction-content {
    width: min(100% - 32px, 780px);
    margin-top: 64px;
  }

  .restriction-details {
    grid-template-columns: 1fr;
    gap: 20px;
  }

  .restriction-contact,
  .restriction-footer {
    flex-direction: column;
  }

  .restriction-contact strong {
    max-width: 100%;
    text-align: left;
  }
}
</style>
