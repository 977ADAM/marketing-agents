import { Routes, Route } from 'react-router-dom'
import { Sidebar } from './components/Sidebar'
import { NewCampaign } from './components/NewCampaign'
import { CampaignView } from './components/CampaignView'
import { useCampaigns } from './hooks/useCampaigns'

export default function App() {
  const { items, refresh } = useCampaigns()
  return (
    <div className="app">
      <Sidebar items={items} />
      <main className="main">
        <Routes>
          <Route path="/" element={<NewCampaign onCreated={refresh} />} />
          <Route path="/campaigns/:id" element={<CampaignView />} />
        </Routes>
      </main>
    </div>
  )
}
