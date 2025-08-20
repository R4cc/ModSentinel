import { Button } from '@/components/ui/Button.jsx';
import { useNavigate } from 'react-router-dom';
import { useDashboardStore } from '@/stores/dashboardStore.js';
import { emitDashboardRefresh } from '@/lib/refresh.js';
import { useOpenAddMod } from '@/hooks/useOpenAddMod.js';

export default function QuickActionsCard() {
  const navigate = useNavigate();
  const { loading, refreshing } = useDashboardStore();
  const openAddMod = useOpenAddMod();
  const checking = loading || refreshing;

  const handleCheck = () => {
    emitDashboardRefresh({ force: true });
  };

  return (
    <div className='flex flex-col gap-md'>
      <Button onClick={openAddMod}>Add mod</Button>
      <Button onClick={handleCheck} disabled={checking} aria-busy={checking}>
        {checking ? 'Checking...' : 'Check updates'}
      </Button>
      <Button onClick={() => navigate('/settings')}>Settings</Button>
    </div>
  );
}
