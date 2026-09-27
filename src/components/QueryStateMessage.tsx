import React from 'react';
import { Alert, AlertVariant, Button } from '@grafana/ui';

export enum QueryStateMessageType {
  ApiKeyNotConfigured = 'apiKeyNotConfigured',
  NoCategoriesSelected = 'noCategoriesSelected',
  TooManyRequests = 'tooManyRequests',
  Unauthorized = 'unauthorized',
  NoData = 'noData',
}

interface QueryStateMessageContent {
  title: string;
  message: string;
  severity: AlertVariant;
}

const QUERY_STATE_MESSAGES: Record<QueryStateMessageType, QueryStateMessageContent> = {
  [QueryStateMessageType.ApiKeyNotConfigured]: {
    title: 'Настройка data source',
    message: 'Настройте подключение в настройках data source',
    severity: 'warning',
  },
  [QueryStateMessageType.NoCategoriesSelected]: {
    title: 'Не выбраны категории',
    message: 'Выберите хотя бы одну категорию',
    severity: 'warning',
  },
  [QueryStateMessageType.TooManyRequests]: {
    title: 'Превышен лимит запросов',
    message: 'Превышен лимит запросов, попробуйте через 60 секунд',
    severity: 'warning',
  },
  [QueryStateMessageType.Unauthorized]: {
    title: 'Ошибка авторизации',
    message: 'Неверный API-ключ, проверьте настройки',
    severity: 'error',
  },
  [QueryStateMessageType.NoData]: {
    title: 'Нет данных',
    message: 'Нет данных за выбранный период',
    severity: 'info',
  },
};

export interface QueryStateMessageProps {
  type?: QueryStateMessageType;
  message?: string;
  onRetry?: () => void;
  title?: string;
}

export function queryStateFromHttpStatus(status?: number): QueryStateMessageType | undefined {
  switch (status) {
    case 401:
      return QueryStateMessageType.Unauthorized;
    case 429:
      return QueryStateMessageType.TooManyRequests;
    default:
      return undefined;
  }
}

export function QueryStateMessage({ type, message, onRetry, title }: QueryStateMessageProps) {
  if (!type) {
    return null;
  }

  const content = QUERY_STATE_MESSAGES[type];

  return (
    <Alert title={title ?? content.title} severity={content.severity}>
      {message ?? content.message}
      {type === QueryStateMessageType.TooManyRequests && onRetry && (
        <Button type="button" onClick={onRetry}>
          Повторить запрос
        </Button>
      )}
    </Alert>
  );
}
