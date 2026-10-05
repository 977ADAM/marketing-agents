// Запросы к API кампаний. Никакой логики: что пришло — то и показываем.

import { creationRequest } from './create.js';
import { postJSON, request, historyPage, type HistoryRows } from './client';
import type { Brief, Campaign, CampaignSummary, CreateRunResponse } from './types';

const create=creationRequest((path,body,key)=>postJSON<CreateRunResponse>(path,body,key));

export function createCampaign(brief: Brief): Promise<CreateRunResponse> {
	return create('campaigns', brief);
}

export function getCampaign(id: string, signal?: AbortSignal): Promise<Campaign> {
	return request<Campaign>(`campaigns/${id}`, { signal });
}

export function listCampaigns(before?: string, signal?: AbortSignal): Promise<HistoryRows<CampaignSummary>> {
	return historyPage<CampaignSummary>('campaigns', before, signal);
}

export function retryCampaign(id:string):Promise<CreateRunResponse>{return postJSON<CreateRunResponse>(`campaigns/${id}/retry`,{});}
