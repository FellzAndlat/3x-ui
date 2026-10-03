import { useLocation } from 'react-router';

import SingBoxPage from './SingBoxPage';
import SingBoxUtilityPage from './SingBoxUtilityPage';

export default function SingBoxRoutePage() {
  const { hash } = useLocation();

  if (hash === '#gateway') return <SingBoxUtilityPage section="gateway" />;
  if (hash === '#adblock') return <SingBoxUtilityPage section="adblock" />;

  return <SingBoxPage />;
}
