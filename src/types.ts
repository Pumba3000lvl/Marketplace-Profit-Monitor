import { DataSourceJsonData } from '@grafana/data';
import { DataQuery } from '@grafana/schema';

export type MarketplaceRoute = 'wb-tariffs' | 'wb-prices' | 'ozon' | 'metrics';
export type RequestMethod = 'GET' | 'POST';
export type MarketplaceSelection = 'wildberries' | 'ozon' | 'both';
export type MarketplaceQueryType = 'commissions' | 'prices' | 'profitability' | 'history' | 'alert';
export type AlertMetric =
  | 'netMarginPercent'
  | 'commissionIncreasePercent'
  | 'competitorPriceDiffPercent'
  | 'storageCostToRevenuePercent'
  | 'netMarginRUB';
export type MarketplaceCategoryId = `wb:${string}` | `ozon:${string}`;

export const MARKETPLACE_OPTIONS = [
  { label: 'Wildberries', value: 'wildberries' },
  { label: 'Ozon', value: 'ozon' },
  { label: 'Оба маркетплейса', value: 'both' },
] as const;

export const QUERY_TYPE_OPTIONS = [
  { label: 'Комиссии', value: 'commissions' },
  { label: 'Цены', value: 'prices' },
  { label: 'Рентабельность', value: 'profitability' },
  { label: 'История', value: 'history' },
  { label: 'Метрика для оповещения', value: 'alert' },
] as const;

export const ALERT_METRIC_OPTIONS: Array<{ label: string; value: AlertMetric }> = [
  { label: 'Чистая маржа (%)', value: 'netMarginPercent' },
  { label: 'Рост комиссии (%)', value: 'commissionIncreasePercent' },
  { label: 'Разница с ценой конкурента (%)', value: 'competitorPriceDiffPercent' },
  { label: 'Расходы на хранение / выручка (%)', value: 'storageCostToRevenuePercent' },
  { label: 'Чистая маржа (₽)', value: 'netMarginRUB' },
];

export interface MarketplaceCategoryOption {
  label: string;
  value: MarketplaceCategoryId;
}

export const MARKETPLACE_CATEGORY_OPTIONS: Record<MarketplaceSelection, MarketplaceCategoryOption[]> = {
  wildberries: [
    { label: 'Одежда и обувь', value: 'wb:apparel-and-shoes' },
    { label: 'Красота и здоровье', value: 'wb:beauty-and-health' },
    { label: 'Электроника', value: 'wb:electronics' },
    { label: 'Дом и сад', value: 'wb:home-and-garden' },
    { label: 'Детские товары', value: 'wb:kids' },
  ],
  ozon: [
    { label: 'Одежда и обувь', value: 'ozon:apparel-and-shoes' },
    { label: 'Красота и здоровье', value: 'ozon:beauty-and-health' },
    { label: 'Электроника', value: 'ozon:electronics' },
    { label: 'Дом и кухня', value: 'ozon:home-and-kitchen' },
    { label: 'Детские товары', value: 'ozon:kids' },
  ],
  both: [
    { label: 'Wildberries — одежда и обувь', value: 'wb:apparel-and-shoes' },
    { label: 'Wildberries — красота и здоровье', value: 'wb:beauty-and-health' },
    { label: 'Wildberries — электроника', value: 'wb:electronics' },
    { label: 'Wildberries — дом и сад', value: 'wb:home-and-garden' },
    { label: 'Wildberries — детские товары', value: 'wb:kids' },
    { label: 'Ozon — одежда и обувь', value: 'ozon:apparel-and-shoes' },
    { label: 'Ozon — красота и здоровье', value: 'ozon:beauty-and-health' },
    { label: 'Ozon — электроника', value: 'ozon:electronics' },
    { label: 'Ozon — дом и кухня', value: 'ozon:home-and-kitchen' },
    { label: 'Ozon — детские товары', value: 'ozon:kids' },
  ],
};

export const MAX_QUERY_LIMIT = 100_000;

export interface MarketplaceQuery extends DataQuery {
  marketplace: MarketplaceRoute;
  path: string;
  method: RequestMethod;
  body?: string;
  selectedMarketplace?: MarketplaceSelection;
  queryType?: MarketplaceQueryType;
  alertMetric?: AlertMetric;
  categories?: MarketplaceCategoryId[];
  limit?: number;
}

export const MARKETPLACE_ROUTES: Array<{ label: string; value: MarketplaceRoute }> = [
  { label: 'Wildberries — API тарифов', value: 'wb-tariffs' },
  { label: 'Wildberries — API цен и скидок', value: 'wb-prices' },
  { label: 'Ozon — API продавца', value: 'ozon' },
  { label: 'Все маркетплейсы — метрики товаров', value: 'metrics' },
];

export const DEFAULT_QUERY: Partial<MarketplaceQuery> = {
  marketplace: 'wb-tariffs',
  path: '/api/v1/tariffs/box',
  method: 'GET',
  selectedMarketplace: 'both',
  queryType: 'profitability',
  categories: [],
  limit: 1000,
};

export interface MarketplaceDataSourceOptions extends DataSourceJsonData {
  ozonClientId?: string;
  telegramChatId?: string;
}

export interface MarketplaceSecureJsonData {
  wildberriesToken?: string;
  ozonApiKey?: string;
  telegramBotToken?: string;
}
