import React, { FormEvent, useState } from 'react';
import { DataSourcePluginOptionsEditorProps } from '@grafana/data';
import { getBackendSrv } from '@grafana/runtime';
import { Alert, Button, InlineField, Input, SecretInput } from '@grafana/ui';
import { MarketplaceDataSourceOptions, MarketplaceSecureJsonData } from '../types';

interface Props
  extends DataSourcePluginOptionsEditorProps<MarketplaceDataSourceOptions, MarketplaceSecureJsonData> {}

type MarketplaceName = 'wildberries' | 'ozon';

interface ConnectionTestResult {
  marketplace: MarketplaceName;
  ok: boolean;
  message: string;
}

interface ConnectionTestResponse {
  results: ConnectionTestResult[];
}

const PLUGIN_ID = 'pumba3000lvl-profitmonitor-datasource';

export function ConfigEditor({ options, onOptionsChange }: Props) {
  const { secureJsonData, secureJsonFields } = options;
  const [originalOzonClientId] = useState(options.jsonData?.ozonClientId ?? '');
  const [testing, setTesting] = useState(false);
  const [results, setResults] = useState<ConnectionTestResult[]>([]);
  const [testError, setTestError] = useState('');

  const jsonData = options.jsonData ?? {};
  const ozonClientId = jsonData.ozonClientId ?? '';
  const telegramChatId = jsonData.telegramChatId ?? '';
  const wildberriesKeyInput = secureJsonData?.wildberriesToken ?? '';
  const ozonKeyInput = secureJsonData?.ozonApiKey ?? '';
  const telegramTokenInput = secureJsonData?.telegramBotToken ?? '';

  const pendingWildberriesKey = Boolean(wildberriesKeyInput.trim());
  const pendingOzonCredentials =
    Boolean(ozonKeyInput.trim()) || ozonClientId !== originalOzonClientId;

  const validationErrors: string[] = [];
  if (wildberriesKeyInput && !wildberriesKeyInput.trim()) {
    validationErrors.push('Укажите непустой API-ключ Wildberries.');
  }
  if (ozonKeyInput && !ozonKeyInput.trim()) {
    validationErrors.push('Укажите непустой API-ключ Ozon.');
  }
  if (ozonClientId && !/^\d+$/.test(ozonClientId.trim())) {
    validationErrors.push('Идентификатор клиента Ozon (Client-Id) должен содержать только цифры.');
  }

  const hasTelegramToken =
    Boolean(telegramTokenInput.trim()) || Boolean(secureJsonFields?.telegramBotToken);
  const hasTelegramChatId = Boolean(telegramChatId.trim());
  if (telegramTokenInput && !/^\d+:[A-Za-z0-9_-]+$/.test(telegramTokenInput.trim())) {
    validationErrors.push('Неверный формат токена Telegram-бота.');
  }
  if (telegramChatId && !/^-?\d+$|^@[A-Za-z0-9_]{5,}$/.test(telegramChatId.trim())) {
    validationErrors.push('Укажите числовой ID чата Telegram или имя канала в формате @channel.');
  }
  if (hasTelegramToken !== hasTelegramChatId) {
    validationErrors.push('Заполните оба поля Telegram или оставьте их пустыми.');
  }

  const savedMarketplaces: MarketplaceName[] = [];
  if (secureJsonFields?.wildberriesToken && !pendingWildberriesKey) {
    savedMarketplaces.push('wildberries');
  }
  if (
    secureJsonFields?.ozonApiKey &&
    !pendingOzonCredentials &&
    /^\d+$/.test(ozonClientId.trim())
  ) {
    savedMarketplaces.push('ozon');
  }
  const hasPendingMarketplaceCredentials = pendingWildberriesKey || pendingOzonCredentials;
  const canTest =
    Boolean(options.uid) &&
    savedMarketplaces.length > 0 &&
    !hasPendingMarketplaceCredentials &&
    !testing;

  const clearTestResult = () => {
    setResults([]);
    setTestError('');
  };

  const setSecret = (key: keyof MarketplaceSecureJsonData, event: FormEvent<HTMLInputElement>) => {
    clearTestResult();
    onOptionsChange({
      ...options,
      secureJsonData: {
        ...secureJsonData,
        [key]: event.currentTarget.value,
      },
    });
  };

  const resetSecret = (key: keyof MarketplaceSecureJsonData) => {
    clearTestResult();
    onOptionsChange({
      ...options,
      secureJsonFields: {
        ...secureJsonFields,
        [key]: false,
      },
      secureJsonData: {
        ...secureJsonData,
        [key]: '',
      },
    });
  };

  const setJSONData = (key: 'ozonClientId' | 'telegramChatId', value: string) => {
    clearTestResult();
    onOptionsChange({
      ...options,
      jsonData: {
        ...jsonData,
        [key]: value,
      },
    });
  };

  const testConnection = async () => {
    if (!canTest || validationErrors.length > 0) {
      return;
    }
    setTesting(true);
    clearTestResult();
    try {
      const response = await getBackendSrv().post<ConnectionTestResponse>(
        `/api/datasources/uid/${encodeURIComponent(options.uid)}/resources/test-connection`,
        { marketplaces: savedMarketplaces },
        {
          headers: {
            'X-Plugin-Id': PLUGIN_ID,
            'X-Datasource-Uid': options.uid,
          },
          showErrorAlert: false,
        }
      );
      setResults(response.results);
    } catch {
      setTestError('Не удалось проверить подключение. Сохраните источник данных и повторите попытку.');
    } finally {
      setTesting(false);
    }
  };

  return (
    <>
      <InlineField label="API-ключ Wildberries" labelWidth={24} tooltip="Хранится в Grafana в зашифрованном виде.">
        <SecretInput
          isConfigured={secureJsonFields?.wildberriesToken ?? false}
          value={wildberriesKeyInput}
          placeholder="Введите API-ключ Wildberries"
          width={40}
          onChange={(event) => setSecret('wildberriesToken', event)}
          onReset={() => resetSecret('wildberriesToken')}
        />
      </InlineField>
      <InlineField label="Ozon Client-Id" labelWidth={24} tooltip="Хранится в настройках источника данных.">
        <Input
          value={ozonClientId}
          placeholder="Введите Client-Id Ozon"
          width={40}
          onChange={(event) => setJSONData('ozonClientId', event.currentTarget.value)}
        />
      </InlineField>
      <InlineField label="API-ключ Ozon" labelWidth={24} tooltip="Хранится в Grafana в зашифрованном виде.">
        <SecretInput
          isConfigured={secureJsonFields?.ozonApiKey ?? false}
          value={ozonKeyInput}
          placeholder="Введите API-ключ Ozon"
          width={40}
          onChange={(event) => setSecret('ozonApiKey', event)}
          onReset={() => resetSecret('ozonApiKey')}
        />
      </InlineField>
      <InlineField
        label="Токен Telegram-бота (устаревшее поле)"
        labelWidth={24}
        tooltip="Для совместимости. Не используется оповещениями; настройте контактные точки через provisioning Grafana."
      >
        <SecretInput
          isConfigured={secureJsonFields?.telegramBotToken ?? false}
          value={telegramTokenInput}
          placeholder="Необязательный токен бота для оповещений"
          width={40}
          onChange={(event) => setSecret('telegramBotToken', event)}
          onReset={() => resetSecret('telegramBotToken')}
        />
      </InlineField>
      <InlineField
        label="ID чата Telegram (устаревшее поле)"
        labelWidth={24}
        tooltip="Для совместимости. Не используется оповещениями; настройте контактные точки через provisioning Grafana."
      >
        <Input
          value={telegramChatId}
          placeholder="Необязательный ID чата для оповещений"
          width={40}
          onChange={(event) => setJSONData('telegramChatId', event.currentTarget.value)}
        />
      </InlineField>

      {validationErrors.map((message) => (
        <Alert key={message} title="Некорректная настройка" severity="error">
          {message}
        </Alert>
      ))}

      <InlineField label="Проверка подключения" labelWidth={24}>
        <Button
          type="button"
          onClick={testConnection}
          disabled={!canTest || validationErrors.length > 0}
          icon={testing ? 'fa fa-spinner' : 'plug'}
        >
          {testing ? 'Проверка…' : 'Проверить подключение'}
        </Button>
      </InlineField>
      {!options.uid && <Alert title="Сначала сохраните источник данных" severity="info" />}
      {hasPendingMarketplaceCredentials && (
        <Alert title="Сохраните изменения перед проверкой" severity="info">
          Для проверки используются сохранённые учётные данные.
        </Alert>
      )}
      {savedMarketplaces.length === 0 && !hasPendingMarketplaceCredentials && (
        <Alert title="Нет сохранённых учётных данных маркетплейсов" severity="info">
          Добавьте учётные данные и сохраните источник данных перед проверкой.
        </Alert>
      )}
      {testError && (
        <Alert title="Не удалось проверить подключение" severity="error">
          {testError}
        </Alert>
      )}
      {results.map((result) => (
        <Alert
          key={result.marketplace}
          title={result.marketplace === 'wildberries' ? 'Wildberries' : 'Ozon'}
          severity={result.ok ? 'success' : 'error'}
        >
          {result.ok ? 'Подключение работает.' : result.message}
        </Alert>
      ))}
    </>
  );
}
