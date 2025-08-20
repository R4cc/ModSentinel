import { useEffect, useState } from 'react';
import { useMatch, useNavigate } from 'react-router-dom';
import { Select } from '@/components/ui/Select.jsx';
import { getInstances } from '@/lib/api.ts';

export default function InstanceSwitcher() {
  const [instances, setInstances] = useState([]);
  const navigate = useNavigate();
  const match = useMatch('/instances/:id/*');
  const currentId = match?.params.id ?? '';

  useEffect(() => {
    async function fetch() {
      try {
        const data = await getInstances();
        setInstances(data);
      } catch {
        // ignore errors
      }
    }
    fetch();
  }, []);

  function handleChange(e) {
    const id = e.target.value;
    if (id) {
      navigate(`/instances/${id}`);
    }
  }

  return (
    <div className="md:ml-auto w-full md:w-48 mt-sm md:mt-0">
      <label htmlFor="instance-switcher" className="sr-only">
        Switch instance
      </label>
      <Select
        id="instance-switcher"
        value={currentId}
        onChange={handleChange}
        className="w-full"
      >
        <option value="" disabled>
          Select instance
        </option>
        {instances.map((inst) => (
          <option key={inst.id} value={inst.id}>
            {inst.name}
          </option>
        ))}
      </Select>
    </div>
  );
}
