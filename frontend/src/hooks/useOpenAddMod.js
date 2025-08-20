import { useNavigate } from 'react-router-dom';
import { useAddModStore } from '@/stores/addModStore.js';

export function useOpenAddMod() {
  const navigate = useNavigate();
  const resetWizard = useAddModStore((s) => s.resetWizard);

  return () => {
    resetWizard();
    navigate('/mods/add');
  };
}

export default useOpenAddMod;
