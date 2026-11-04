<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue';

import { IconifyIcon } from '@vben/icons';

import { Button, Card, message, Switch, Tag } from 'ant-design-vue';

import { getSecuritySettings, updateSecuritySettings } from '#/api/ipam';
import PageHeader from '#/components/page-header.vue';
import StatCard from '#/components/stat-card.vue';
import { $t } from '#/locales';

defineOptions({ name: 'SystemSecurity' });

const security = reactive({ forcePasswordChange: false });
const loading = ref(false);
const saving = ref(false);

async function load() {
  loading.value = true;
  try {
    const s = await getSecuritySettings();
    security.forcePasswordChange = Boolean(s.forcePasswordChange);
  } catch (error) {
    message.error(
      error instanceof Error ? error.message : $t('system.security.msg.loadFailed'),
    );
  } finally {
    loading.value = false;
  }
}

async function save() {
  saving.value = true;
  try {
    const saved = await updateSecuritySettings(security.forcePasswordChange);
    security.forcePasswordChange = Boolean(saved.forcePasswordChange);
    message.success(
      saved.forcePasswordChange
        ? $t('system.security.msg.enabled')
        : $t('system.security.msg.disabled'),
    );
  } catch (error) {
    message.error(
      error instanceof Error ? error.message : $t('system.security.msg.saveFailed'),
    );
  } finally {
    saving.value = false;
  }
}

onMounted(load);
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="System"
      icon="lucide:key-round"
      :title="$t('system.security.page.title')"
      :description="$t('system.security.page.desc')"
    >
      <template #actions>
        <Button size="small" :loading="loading" @click="load">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />{{
            $t('system.security.actions.refresh')
          }}
        </Button>
        <Button type="primary" size="small" :loading="saving" @click="save">
          <IconifyIcon icon="lucide:save" class="mr-1 size-3.5" />{{
            $t('system.security.actions.save')
          }}
        </Button>
      </template>
    </PageHeader>

    <div class="ipam-stat-grid">
      <StatCard
        :label="$t('system.security.stat.force')"
        :value="
          security.forcePasswordChange
            ? $t('system.security.stat.on')
            : $t('system.security.stat.off')
        "
        icon="lucide:shield-check"
        :tone="security.forcePasswordChange ? 'green' : 'violet'"
        :hint="
          security.forcePasswordChange
            ? $t('system.security.stat.forceHintOn')
            : $t('system.security.stat.forceHintOff')
        "
      />
      <StatCard
        :label="$t('system.security.stat.minLength')"
        value="10"
        :unit="$t('system.security.stat.digitUnit')"
        icon="lucide:ruler"
        tone="blue"
        :hint="$t('system.security.stat.minLengthHint')"
      />
      <StatCard
        :label="$t('system.security.stat.classes')"
        value="3 / 4"
        icon="lucide:case-sensitive"
        tone="cyan"
        :hint="$t('system.security.stat.classesHint')"
      />
      <StatCard
        :label="$t('system.security.stat.protection')"
        :value="$t('system.security.stat.protectionValue')"
        icon="lucide:gauge"
        tone="amber"
        :hint="$t('system.security.stat.protectionHint')"
      />
    </div>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:shield-check" />{{
            $t('system.security.card.switchTitle')
          }}
        </span>
      </template>
      <template #extra>
        <Tag
          class="m-0"
          :color="security.forcePasswordChange ? 'green' : 'default'"
        >
          {{
            security.forcePasswordChange
              ? $t('system.security.card.enabled')
              : $t('system.security.card.disabled')
          }}
        </Tag>
      </template>

      <div class="ipam-form-grid">
        <div class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">{{
            $t('system.security.card.switchLabel')
          }}</span>
          <div class="flex items-center gap-3">
            <Switch
              v-model:checked="security.forcePasswordChange"
              size="small"
            />
            <span class="ipam-hint">
              {{
                security.forcePasswordChange
                  ? $t('system.security.card.onDesc')
                  : $t('system.security.card.offDesc')
              }}
            </span>
          </div>
        </div>
        <div class="ipam-form-hint ipam-form-field--wide">
          {{ $t('system.security.card.hint') }}
        </div>
      </div>

      <div class="ipam-toolbar ipam-toolbar--plain mt-3">
        <span class="ipam-hint">{{ $t('system.security.card.footerHint') }}</span>
        <span class="ipam-toolbar__spacer"></span>
        <Button type="primary" :loading="saving" @click="save">
          <IconifyIcon icon="lucide:save" class="mr-1 size-3.5" />{{
            $t('system.security.actions.save')
          }}
        </Button>
      </div>
    </Card>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:list-checks" />{{
            $t('system.security.fixed.title')
          }}
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">{{ $t('system.security.fixed.hint') }}</span>
      </template>
      <div class="ipam-form-grid">
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('system.security.fixed.length')
          }}</span>
          <div class="ipam-service-value">
            <span class="ipam-mono">{{ $t('system.security.fixed.lengthValue') }}</span>
          </div>
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('system.security.fixed.complexity')
          }}</span>
          <div class="ipam-service-value">
            <span>{{ $t('system.security.fixed.complexityValue') }}</span>
          </div>
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('system.security.fixed.banned')
          }}</span>
          <div class="ipam-service-value">
            <span>{{ $t('system.security.fixed.bannedValue') }}</span>
          </div>
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('system.security.fixed.afterChange')
          }}</span>
          <div class="ipam-service-value">
            <span>{{ $t('system.security.fixed.afterChangeValue') }}</span>
          </div>
        </div>
      </div>
    </Card>
  </div>
</template>
