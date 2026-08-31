import { Routes, Route } from 'react-router-dom'
import { Sidebar } from './components/Sidebar'
import { NewCampaign } from './components/NewCampaign'
import { CampaignView } from './components/CampaignView'
import { ReviewForm } from './components/ReviewForm'
import { ReviewView } from './components/ReviewView'
import { useCampaigns } from './hooks/useCampaigns'
import { useReviews } from './hooks/useReviews'

export default function App() {
  const { items: campaigns, refresh: refreshCampaigns } = useCampaigns()
  const { items: reviews, refresh: refreshReviews } = useReviews()
  return (
    <div className="app">
      <Sidebar campaigns={campaigns} reviews={reviews} />
      <main className="main">
        <Routes>
          <Route path="/" element={<NewCampaign onCreated={refreshCampaigns} />} />
          <Route path="/campaigns/:id" element={<CampaignView />} />
          <Route path="/reviews" element={<ReviewForm onCreated={refreshReviews} />} />
          <Route path="/reviews/:id" element={<ReviewView />} />
        </Routes>
      </main>
    </div>
  )
}
