<script setup lang="ts">
import type { LDAPSettings } from '#/api/ipam';

import { computed, onMounted, reactive, ref } from 'vue';

import { IconifyIcon } from '@vben/icons';

import {
  Alert,
  Button,
  Card,
  Input,
  InputPassword,
  message,
  Switch,
  Table,
  Tag,
} from 'ant-design-vue';

import {
  getLDAPSettings,
  listRoles,
  testLDAPLogin,
  updateLDAPSettings,
} from '#/api/ipam';
import PageHeader from '#/components/page-header.vue';
import StatCard from '#/components/stat-card.vue';
import { $t } from '#/locales';

defineOptions({ name: 'SystemDirectory' });

const loading = ref(false);
const saving = ref(false);
const testing = ref(false);
const bindPasswordInput = ref('');
const roleNames = ref<string[]>(['admin', 'operator', 'auditor', 'user']);
const testResult = ref<null | {
  displayName: string;
  email?: string;
  groups: string[];
  roles: string[];
  username: string;
}>(null);

const form = reactive<LDAPSettings>({
  enabled: false,
  serverUrl: 'ldap://127.0.0.1:389',
  baseDn: '',
  userFilter: '(uid={username})',
  defaultRole: 'user',
  roleMap: {},
  startTls: false,
  insecureSkipVerify: false,
  groupFilter: '(&(objectClass=groupOfNames)(member={dn}))',
  groupAttr: 'cn',
});

const test = reactive({ username: '', password: '' });
const newGroup = ref('');
const newRole = ref('operator');

const roleMapRows = computed(() =>
  Object.entries(form.roleMap ?? {}).map(([group, role]) => ({ group, role })),
);

function apply(s: LDAPSettings) {
  form.enabled = Boolean(s.enabled);
  form.serverUrl = s.serverUrl || 'ldap://127.0.0.1:389';
  form.baseDn = s.baseDn || '';
  form.userFilter = s.userFilter || '(uid={username})';
  form.defaultRole = s.defaultRole || 'user';
  form.roleMap = { ...s.roleMap };
  form.startTls = Boolean(s.startTls);
  form.insecureSkipVerify = Boolean(s.insecureSkipVerify);
  form.bindDn = s.bindDn || '';
  form.hasBindPassword = Boolean(s.hasBindPassword);
  form.groupBaseDn = s.groupBaseDn || '';
  form.groupFilter =
    s.groupFilter || '(&(objectClass=groupOfNames)(member={dn}))';
  form.groupAttr = s.groupAttr || 'cn';
}

async function load() {
  loading.value = true;
  try {
    apply(await getLDAPSettings());
    try {
      const roles = await listRoles();
      const names = (roles.items ?? []).map((r) => r.name);
      if (names.length > 0) {
        roleNames.value = [...new Set([...roleNames.value, ...names])];
      }
    } catch {
      /* 角色列表失败不阻塞设置读取 */
    }
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('system.directory.msg.loadFailed'),
    );
  } finally {
    loading.value = false;
  }
}

async function save() {
  if (form.enabled && (!form.serverUrl || !form.baseDn)) {
    message.warning($t('system.directory.msg.required'));
    return;
  }
  saving.value = true;
  try {
    const body: LDAPSettings = { ...form };
    if (bindPasswordInput.value) {
      body.bindPassword = bindPasswordInput.value;
    } else {
      delete body.bindPassword;
    }
    apply(await updateLDAPSettings(body));
    bindPasswordInput.value = '';
    message.success($t('system.directory.msg.saved'));
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('system.directory.msg.saveFailed'),
    );
  } finally {
    saving.value = false;
  }
}

async function runTest() {
  if (!test.username || !test.password) {
    message.warning($t('system.directory.msg.testRequired'));
    return;
  }
  testing.value = true;
  testResult.value = null;
  try {
    testResult.value = await testLDAPLogin(test.username, test.password);
    message.success($t('system.directory.msg.testOk'));
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('system.directory.msg.testFailed'),
    );
  } finally {
    testing.value = false;
  }
}

function addMapping() {
  const group = newGroup.value.trim();
  if (!group) return;
  form.roleMap = { ...form.roleMap, [group]: newRole.value };
  newGroup.value = '';
}

function removeMapping(group: string) {
  const next = { ...form.roleMap };
  delete next[group];
  form.roleMap = next;
}

onMounted(load);
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="System"
      icon="lucide:network"
      :title="$t('system.directory.page.title')"
      :description="$t('system.directory.page.desc')"
    >
      <template #actions>
        <Button size="small" :loading="loading" @click="load">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />{{
            $t('system.directory.actions.refresh')
          }}
        </Button>
        <Button type="primary" size="small" :loading="saving" @click="save">
          <IconifyIcon icon="lucide:save" class="mr-1 size-3.5" />{{
            $t('system.directory.actions.save')
          }}
        </Button>
      </template>
    </PageHeader>

    <div class="ipam-stat-grid">
      <StatCard
        :label="$t('system.directory.stat.service')"
        :value="
          form.enabled
            ? $t('system.directory.stat.enabled')
            : $t('system.directory.stat.disabled')
        "
        icon="lucide:network"
        :tone="form.enabled ? 'green' : 'violet'"
        :hint="
          form.enabled
            ? $t('system.directory.stat.serviceHintOn')
            : $t('system.directory.stat.serviceHintOff')
        "
      />
      <StatCard
        :label="$t('system.directory.stat.source')"
        :value="$t('system.directory.stat.sourceValue')"
        icon="lucide:users"
        tone="blue"
        :hint="$t('system.directory.stat.sourceHint')"
      />
      <StatCard
        :label="$t('system.directory.stat.mapping')"
        :value="
          $t('system.directory.stat.mappingValue', { n: roleMapRows.length })
        "
        icon="lucide:shield-check"
        tone="cyan"
        :hint="
          $t('system.directory.stat.mappingHint', {
            role: form.defaultRole || 'user',
          })
        "
      />
      <StatCard
        :label="$t('system.directory.stat.binding')"
        :value="$t('system.directory.stat.bindingValue')"
        icon="lucide:key-round"
        tone="amber"
        :hint="$t('system.directory.stat.bindingHint')"
      />
    </div>

    <Alert
      v-if="form.enabled && form.insecureSkipVerify"
      class="mb-3"
      :message="$t('system.directory.alert.tlsTitle')"
      :description="$t('system.directory.alert.tlsDesc')"
      show-icon
      type="warning"
    />

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:server" />{{
            $t('system.directory.connection.title')
          }}
        </span>
      </template>
      <template #extra>
        <Tag class="m-0" :color="form.enabled ? 'green' : 'default'">
          {{
            form.enabled
              ? $t('system.directory.tag.enabled')
              : $t('system.directory.tag.disabled')
          }}
        </Tag>
      </template>

      <div class="ipam-form-grid">
        <div class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">{{
            $t('system.directory.connection.enable')
          }}</span>
          <div class="flex items-center gap-3">
            <Switch v-model:checked="form.enabled" size="small" />
            <span class="ipam-hint">
              {{ $t('system.directory.connection.enableHint') }}
            </span>
          </div>
        </div>

        <div class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">{{
            $t('system.directory.connection.server')
          }}</span>
          <Input
            v-model:value="form.serverUrl"
            :placeholder="$t('system.directory.connection.serverPlaceholder')"
          />
          <span class="ipam-hint">
            {{ $t('system.directory.connection.serverHint') }}
          </span>
        </div>

        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('system.directory.connection.startTls')
          }}</span>
          <div class="flex items-center gap-2">
            <Switch v-model:checked="form.startTls" size="small" />
            <span class="ipam-hint">{{
              $t('system.directory.connection.startTlsHint')
            }}</span>
          </div>
        </div>

        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('system.directory.connection.skipVerify')
          }}</span>
          <div class="flex items-center gap-2">
            <Switch v-model:checked="form.insecureSkipVerify" size="small" />
            <span class="ipam-hint">{{
              $t('system.directory.connection.skipVerifyHint')
            }}</span>
          </div>
        </div>

        <div class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">{{
            $t('system.directory.connection.baseDn')
          }}</span>
          <Input v-model:value="form.baseDn" placeholder="dc=corp,dc=local" />
          <span class="ipam-hint">{{
            $t('system.directory.connection.baseDnHint')
          }}</span>
        </div>

        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('system.directory.connection.bindDn')
          }}</span>
          <Input
            v-model:value="form.bindDn"
            placeholder="cn=svc-ipam,ou=service,dc=corp,dc=local"
          />
          <span class="ipam-hint">{{
            $t('system.directory.connection.bindDnHint')
          }}</span>
        </div>

        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('system.directory.connection.bindPassword')
          }}</span>
          <InputPassword
            v-model:value="bindPasswordInput"
            :placeholder="
              form.hasBindPassword
                ? $t('system.directory.connection.bindPasswordSet')
                : $t('system.directory.connection.bindPasswordEmpty')
            "
            autocomplete="new-password"
          />
          <span class="ipam-hint">
            {{ $t('system.directory.connection.bindPasswordHint') }}
          </span>
        </div>

        <div class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">{{
            $t('system.directory.connection.userFilter')
          }}</span>
          <Input
            v-model:value="form.userFilter"
            placeholder="(uid={username})"
          />
          <span class="ipam-hint">
            {{
              $t('system.directory.connection.userFilterHint', {
                ph: '{username}',
              })
            }}
          </span>
        </div>
      </div>
    </Card>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:shield-check" />{{
            $t('system.directory.mapping.title')
          }}
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">{{ $t('system.directory.mapping.hint') }}</span>
      </template>

      <div class="ipam-form-grid">
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('system.directory.mapping.groupBaseDn')
          }}</span>
          <Input
            v-model:value="form.groupBaseDn"
            placeholder="ou=groups,dc=corp,dc=local"
          />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('system.directory.mapping.groupAttr')
          }}</span>
          <Input v-model:value="form.groupAttr" placeholder="cn" />
        </div>
        <div class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">{{
            $t('system.directory.mapping.groupFilter')
          }}</span>
          <Input
            v-model:value="form.groupFilter"
            placeholder="(&(objectClass=groupOfNames)(member={dn}))"
          />
          <span class="ipam-hint">
            {{
              $t('system.directory.mapping.groupFilterHint', {
                ph: '{dn}',
              })
            }}
          </span>
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('system.directory.mapping.defaultRole')
          }}</span>
          <Input v-model:value="form.defaultRole" placeholder="user" />
          <span class="ipam-hint">{{
            $t('system.directory.mapping.rolesAvailable', {
              roles: roleNames.join(' / '),
            })
          }}</span>
        </div>
      </div>

      <div class="ipam-toolbar ipam-toolbar--plain mt-3">
        <Input
          v-model:value="newGroup"
          :placeholder="$t('system.directory.mapping.newGroupPlaceholder')"
          style="max-width: 420px"
          @press-enter="addMapping"
        />
        <Input
          v-model:value="newRole"
          :placeholder="$t('system.directory.mapping.newRolePlaceholder')"
          style="width: 140px"
        />
        <Button size="small" @click="addMapping">
          <IconifyIcon icon="lucide:plus" class="mr-1 size-3.5" />{{
            $t('system.directory.mapping.add')
          }}
        </Button>
      </div>

      <Table
        :data-source="roleMapRows"
        :locale="{ emptyText: $t('system.directory.mapping.empty') }"
        :pagination="false"
        :row-key="(r: { group: string }) => r.group"
        size="small"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'group'">
            <span class="ipam-mono">{{ record.group }}</span>
          </template>
          <template v-else-if="column.key === 'role'">
            <Tag class="m-0" color="blue">{{ record.role }}</Tag>
          </template>
          <template v-else-if="column.key === 'op'">
            <Button size="small" danger @click="removeMapping(record.group)">
              {{ $t('system.directory.mapping.remove') }}
            </Button>
          </template>
        </template>
        <Table.Column
          key="group"
          :title="$t('system.directory.mapping.colGroup')"
        />
        <Table.Column
          key="role"
          :title="$t('system.directory.mapping.colRole')"
          width="160"
        />
        <Table.Column
          key="op"
          :title="$t('system.directory.mapping.colOp')"
          width="100"
        />
      </Table>
    </Card>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:plug-zap" />{{
            $t('system.directory.test.title')
          }}
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">{{ $t('system.directory.test.hint') }}</span>
      </template>

      <div class="ipam-form-grid">
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('system.directory.test.username')
          }}</span>
          <Input v-model:value="test.username" placeholder="zhangsan" />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('system.directory.test.password')
          }}</span>
          <InputPassword
            v-model:value="test.password"
            autocomplete="off"
            @press-enter="runTest"
          />
        </div>
      </div>

      <div class="ipam-toolbar ipam-toolbar--plain mt-3">
        <span class="ipam-hint">{{ $t('system.directory.test.before') }}</span>
        <span class="ipam-toolbar__spacer"></span>
        <Button type="primary" :loading="testing" @click="runTest">
          <IconifyIcon icon="lucide:plug-zap" class="mr-1 size-3.5" />{{
            $t('system.directory.test.submit')
          }}
        </Button>
      </div>

      <div v-if="testResult" class="mt-3">
        <div class="ipam-service-value">
          <span>
            {{ testResult.displayName }}（{{ testResult.username }}）
            <span v-if="testResult.email" class="ipam-hint">
              · {{ testResult.email }}
            </span>
          </span>
        </div>
        <div class="mt-2 flex flex-wrap items-center gap-2">
          <span class="ipam-hint">{{
            $t('system.directory.test.mappedRoles')
          }}</span>
          <Tag v-for="r in testResult.roles" :key="r" class="m-0" color="green">
            {{ r }}
          </Tag>
        </div>
        <div class="mt-2 flex flex-wrap items-center gap-2">
          <span class="ipam-hint">{{
            $t('system.directory.test.groups')
          }}</span>
          <Tag v-for="g in testResult.groups" :key="g" class="m-0">
            {{ g }}
          </Tag>
        </div>
      </div>
    </Card>
  </div>
</template>
