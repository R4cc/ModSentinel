import { useNavigate } from 'react-router-dom';
import { useAddModStore } from '@/stores/addModStore.js';

export function useOpenAddMod(instanceId) {
  const navigate = useNavigate();
  const resetWizard = useAddModStore((s) => s.resetWizard);

  return () => {
    resetWizard();
    navigate(`/instances/${instanceId}/add`);
  };
}

export default useOpenAddMod;
