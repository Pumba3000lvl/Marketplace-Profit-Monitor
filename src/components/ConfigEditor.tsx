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
    validationErrors.push('Enter a non-empty Wildberries API key.');
  }
  if (ozonKeyInput && !ozonKeyInput.trim()) {
    validationErrors.push('Enter a non-empty Ozon API key.');
  }
  if (ozonClientId && !/^\d+$/.test(ozonClientId.trim())) {
    validationErrors.push('Ozon Client-Id must contain digits only.');
  }

  const hasTelegramToken =
    Boolean(telegramTokenInput.trim()) || Boolean(secureJsonFields?.telegramBotToken);
  const hasTelegramChatId = Boolean(telegramChatId.trim());
  if (telegramTokenInput && !/^\d+:[A-Za-z0-9_-]+$/.test(telegramTokenInput.trim())) {
    validationErrors.push('Telegram Bot Token has an invalid format.');
  }
  if (telegramChatId && !/^-?\d+$|^@[A-Za-z0-9_]{5,}$/.test(telegramChatId.trim())) {
    validationErrors.push('Telegram Chat ID must be a numeric ID or @channel username.');
  }
  if (hasTelegramToken !== hasTelegramChatId) {
    validationErrors.push('Configure both Telegram alert fields, or leave both empty.');
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
      setTestError('Unable to run the connection test. Save the data source and try again.');
    } finally {
      setTesting(false);
    }
  };

  return (
    <>
      <InlineField label="Wildberries API Key" labelWidth={24} tooltip="Stored encrypted by Grafana.">
        <SecretInput
          isConfigured={secureJsonFields?.wildberriesToken ?? false}
          value={wildberriesKeyInput}
          placeholder="Enter Wildberries API key"
          width={40}
          onChange={(event) => setSecret('wildberriesToken', event)}
          onReset={() => resetSecret('wildberriesToken')}
        />
      </InlineField>
      <InlineField label="Ozon Client-Id" labelWidth={24} tooltip="Stored in datasource settings.">
        <Input
          value={ozonClientId}
          placeholder="Enter Ozon Client-Id"
          width={40}
          onChange={(event) => setJSONData('ozonClientId', event.currentTarget.value)}
        />
      </InlineField>
      <InlineField label="Ozon API Key" labelWidth={24} tooltip="Stored encrypted by Grafana.">
        <SecretInput
          isConfigured={secureJsonFields?.ozonApiKey ?? false}
          value={ozonKeyInput}
          placeholder="Enter Ozon API key"
          width={40}
          onChange={(event) => setSecret('ozonApiKey', event)}
          onReset={() => resetSecret('ozonApiKey')}
        />
      </InlineField>
      <InlineField label="Telegram Bot Token" labelWidth={24} tooltip="Stored encrypted by Grafana.">
        <SecretInput
          isConfigured={secureJsonFields?.telegramBotToken ?? false}
          value={telegramTokenInput}
          placeholder="Optional alert bot token"
          width={40}
          onChange={(event) => setSecret('telegramBotToken', event)}
          onReset={() => resetSecret('telegramBotToken')}
        />
      </InlineField>
      <InlineField label="Telegram Chat ID" labelWidth={24} tooltip="Stored in datasource settings.">
        <Input
          value={telegramChatId}
          placeholder="Optional alert chat ID"
          width={40}
          onChange={(event) => setJSONData('telegramChatId', event.currentTarget.value)}
        />
      </InlineField>

      {validationErrors.map((message) => (
        <Alert key={message} title="Invalid configuration" severity="error">
          {message}
        </Alert>
      ))}

      <InlineField label="Connection test" labelWidth={24}>
        <Button
          type="button"
          onClick={testConnection}
          disabled={!canTest || validationErrors.length > 0}
          icon={testing ? 'fa fa-spinner' : 'plug'}
        >
          {testing ? 'Testing…' : 'Test connection'}
        </Button>
      </InlineField>
      {!options.uid && <Alert title="Save the data source before testing" severity="info" />}
      {hasPendingMarketplaceCredentials && (
        <Alert title="Save changes before testing" severity="info">
          The connection test uses saved credentials.
        </Alert>
      )}
      {savedMarketplaces.length === 0 && !hasPendingMarketplaceCredentials && (
        <Alert title="No saved marketplace credentials" severity="info">
          Add credentials and save the data source before testing.
        </Alert>
      )}
      {testError && (
        <Alert title="Connection test failed" severity="error">
          {testError}
        </Alert>
      )}
      {results.map((result) => (
        <Alert
          key={result.marketplace}
          title={result.marketplace === 'wildberries' ? 'Wildberries' : 'Ozon'}
          severity={result.ok ? 'success' : 'error'}
        >
          {result.ok ? 'Connection successful.' : result.message}
        </Alert>
      ))}
    </>
  );
}
