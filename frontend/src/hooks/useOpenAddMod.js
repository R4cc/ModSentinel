import { useNavigate } from 'react-router-dom';
import { useAddModStore } from '@/stores/addModStore.js';

import { parseJarFilename } from '@/lib/jar.ts';

export function useOpenAddMod(instanceId) {
  const navigate = useNavigate();
  const resetWizard = useAddModStore((s) => s.resetWizard);
  const setUrl = useAddModStore((s) => s.setUrl);

  return (arg) => {
    const file = typeof arg === 'string' ? arg : undefined;
    resetWizard();
    if (file) {
      const { slug } = parseJarFilename(file);
      if (slug) {
        setUrl(`https://modrinth.com/mod/${slug}`);
      }
    }
    navigate(`/instances/${instanceId}/add`, { state: { file } });
  };
}

export default useOpenAddMod;
