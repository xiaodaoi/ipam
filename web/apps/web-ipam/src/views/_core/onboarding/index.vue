<script setup lang="ts">
import type { OnboardingCheck } from './types';

import { computed, onMounted, ref } from 'vue';
import { useRouter } from 'vue-router';

import { IconifyIcon } from '@vben/icons';
import { useUserStore } from '@vben/stores';

import { Button, Card, message, Progress, Tag } from 'ant-design-vue';

import {
  listDnsRecords,
  listDnsZones,
  listOrgs,
  listSubnets,
  listUpstreams,
} from '#/api/ipam';
import PageHeader from '#/components/page-header.vue';
import {
  readOnboarding,
  useOnboardingUsername,
  writeOnboarding,
} from '#/composables/use-onboarding';
import { $t } from '#/locales';

interface StepDef {
  description: string;
  icon: string;
  key: string;
  path: string;
  title: string;
}

const router = useRouter();
const userStore = useUserStore();
const username = useOnboardingUsername();

const state = ref(readOnboarding(username));
const loading = ref(true);
const results = ref<Record<string, OnboardingCheck>>({});

const steps = computed<StepDef[]>(() => [
  {
    key: 'org',
    icon: 'lucide:network',
    title: $t('page.onboarding.steps.org.title'),
    description: $t('page.onboarding.steps.org.desc'),
    path: '/system/orgs',
  },
  {
    key: 'subnet',
    icon: 'lucide:blocks',
    title: $t('page.onboarding.steps.subnet.title'),
    description: $t('page.onboarding.steps.subnet.desc'),
    path: '/dhcp/subnets',
  },
  {
    key: 'upstream',
    icon: 'lucide:globe',
    title: $t('page.onboarding.steps.upstream.title'),
    description: $t('page.onboarding.steps.upstream.desc'),
    path: '/dns-service/upstreams',
  },
  {
    key: 'records',
    icon: 'lucide:file-code',
    title: $t('page.onboarding.steps.records.title'),
    description: $t('page.onboarding.steps.records.desc'),
    path: '/dns-service/records',
  },
  {
    key: 'security',
    icon: 'lucide:shield-check',
    title: $t('page.onboarding.steps.security.title'),
    description: $t('page.onboarding.steps.security.desc'),
    path: '/profile',
  },
]);

const doneCount = computed(
  () => steps.value.filter((s) => results.value[s.key]?.done).length,
);
const allDone = computed(
  () => steps.value.length > 0 && doneCount.value === steps.value.length,
);
const percent = computed(() =>
  Math.round((doneCount.value / Math.max(1, steps.value.length)) * 100),
);

async function checkOrg(): Promise<OnboardingCheck> {
  const orgs = await listOrgs();
  return { done: orgs.length > 0, hint: $t('page.onboarding.hints.orgs', { n: orgs.length }) };
}
async function checkSubnet(): Promise<OnboardingCheck> {
  const data = await listSubnets();
  const n = data.items?.length ?? 0;
  return { done: n > 0, hint: $t('page.onboarding.hints.subnets', { n }) };
}
async function checkUpstream(): Promise<OnboardingCheck> {
  const data = await listUpstreams();
  const n = (data.items ?? []).filter((u) => u.enabled).length;
  return { done: n > 0, hint: $t('page.onboarding.hints.upstreams', { n }) };
}
async function checkRecords(): Promise<OnboardingCheck> {
  const zoneData = await listDnsZones();
  const zones = zoneData.items ?? [];
  if (zones.length === 0) {
    return { done: false, hint: $t('page.onboarding.hints.records', { n: 0 }) };
  }
  let records = 0;
  for (const zone of zones.slice(0, 5)) {
    try {
      const data = await listDnsRecords(zone.id);
      records += data.items?.length ?? 0;
      if (records > 0) break;
    } catch {
      // 单个区域读取失败不影响整体判断。
    }
  }
  return { done: records > 0, hint: $t('page.onboarding.hints.records', { n: records }) };
}
function checkSecurity(): OnboardingCheck {
  const info = userStore.userInfo;
  const done = Boolean(info?.totpEnabled) || info?.mustChangePassword === false;
  return { done, hint: $t('page.onboarding.hints.security') };
}

async function runChecks() {
  loading.value = true;
  const checks: Record<string, () => OnboardingCheck | Promise<OnboardingCheck>> = {
    org: checkOrg,
    subnet: checkSubnet,
    upstream: checkUpstream,
    records: checkRecords,
    security: checkSecurity,
  };
  const next: Record<string, OnboardingCheck> = {};
  await Promise.all(
    Object.entries(checks).map(async ([key, fn]) => {
      try {
        next[key] = await fn();
      } catch (error) {
        next[key] = {
          done: false,
          error:
            error instanceof Error
              ? error.message
              : $t('page.onboarding.checkFailed'),
        };
      }
    }),
  );
  results.value = next;
  loading.value = false;
}

function persist(patch: { completed?: boolean; dismissed?: boolean }) {
  state.value = writeOnboarding(username, patch);
}
function finish() {
  if (!allDone.value) {
    message.warning($t('page.onboarding.finishHint'));
    return;
  }
  persist({ completed: true, dismissed: false });
  message.success($t('page.onboarding.finished'));
}
function dismiss() {
  persist({ dismissed: true });
  message.success($t('page.onboarding.dismissed'));
}
function reset() {
  persist({ completed: false, dismissed: false });
  void runChecks();
}
function goto(path: string) {
  void router.push(path);
}

onMounted(() => {
  void runChecks();
});
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="Getting Started"
      icon="lucide:rocket"
      :title="$t('page.onboarding.title')"
      :description="$t('page.onboarding.desc')"
    >
      <template #actions>
        <Button :loading="loading" @click="runChecks">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />
          {{ $t('page.onboarding.recheck') }}
        </Button>
      </template>
    </PageHeader>

    <Card class="mb-4">
      <div class="flex flex-wrap items-center gap-6">
        <div class="min-w-[220px] flex-1">
          <div class="mb-2 flex items-center justify-between text-sm">
            <span class="font-medium">{{ $t('page.onboarding.progress') }}</span>
            <span class="ipam-hint">{{ doneCount }} / {{ steps.length }}</span>
          </div>
          <Progress :percent="percent" :show-info="false" />
          <div class="ipam-hint mt-2">
            {{ allDone ? $t('page.onboarding.allDone') : $t('page.onboarding.inProgress') }}
          </div>
        </div>
        <div class="flex items-center gap-2">
          <Button v-if="state.completed || state.dismissed" @click="reset">
            {{ $t('page.onboarding.restart') }}
          </Button>
          <Button v-else @click="dismiss">{{ $t('page.onboarding.dismiss') }}</Button>
          <Button type="primary" :disabled="!allDone" @click="finish">
            {{ $t('page.onboarding.finish') }}
          </Button>
        </div>
      </div>
    </Card>

    <Card>
      <div class="flex flex-col gap-3">
        <div
          v-for="(step, index) in steps"
          :key="step.key"
          class="ipam-onboarding-step"
          :data-done="results[step.key]?.done ? 'true' : 'false'"
        >
          <div class="ipam-onboarding-step__index">
            <IconifyIcon
              :icon="results[step.key]?.done ? 'lucide:check' : step.icon"
              class="size-4"
            />
          </div>
          <div class="min-w-0 flex-1">
            <div class="flex flex-wrap items-center gap-2">
              <span class="font-medium">{{ index + 1 }}. {{ step.title }}</span>
              <Tag
                :color="results[step.key]?.done ? 'green' : 'default'"
                class="m-0"
              >
                {{ results[step.key]?.done ? $t('page.onboarding.done') : $t('page.onboarding.todo') }}
              </Tag>
              <span v-if="results[step.key]?.error" class="ipam-hint text-amber-600">
                {{ results[step.key]?.error }}
              </span>
              <span v-else-if="results[step.key]?.hint" class="ipam-hint">
                {{ results[step.key]?.hint }}
              </span>
            </div>
            <div class="ipam-hint mt-1">{{ step.description }}</div>
          </div>
          <Button size="small" @click="goto(step.path)">
            {{ $t('page.onboarding.open') }}
          </Button>
        </div>
      </div>
    </Card>

    <div class="ipam-note mt-4">
      <IconifyIcon icon="lucide:info" class="mt-0.5 size-3.5 shrink-0" />
      <span>{{ $t('page.onboarding.note') }}</span>
    </div>
  </div>
</template>

<style scoped>
.ipam-onboarding-step {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px;
  border: 1px solid var(--ant-color-border-secondary, #f0f0f0);
  border-radius: 10px;
}

.ipam-onboarding-step[data-done='true'] {
  border-color: var(--ant-color-success-border, #b7eb8f);
  background: var(--ant-color-success-bg, #f6ffed);
}

.ipam-onboarding-step__index {
  display: grid;
  width: 32px;
  height: 32px;
  flex: none;
  place-items: center;
  border-radius: 50%;
  background: var(--ant-color-fill-secondary, #f5f5f5);
  color: var(--ant-color-primary, #1677ff);
}
</style>
