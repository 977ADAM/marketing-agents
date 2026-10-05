// Запросы к API кампаний. Никакой логики: что пришло — то и показываем.

import { creationRequest } from './create.js';
import { postJSON, request } from './client';
import type { Brief, Campaign, CampaignSummary, CreateRunResponse } from './types';

const create=creationRequest((path,body,key)=>postJSON<CreateRunResponse>(path,body,key));

export function createCampaign(brief: Brief): Promise<CreateRunResponse> {
	return create('campaigns', brief);
}

export function getCampaign(id: string): Promise<Campaign> {
	return request<Campaign>(`campaigns/${id}`);
}

export function listCampaigns(): Promise<CampaignSummary[]> {
	return request<CampaignSummary[]>('campaigns');
}
