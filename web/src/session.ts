import { createContext, useContext } from 'react'
import type { Me } from './api'

/** MeContext carries the signed-in user to every screen. */
export const MeContext = createContext<Me | null>(null)

export const useMe = () => useContext(MeContext)

/** useCanRun is whether the signed-in account may change anything (operator or admin). The server enforces this
 *  regardless; screens use it to offer only the controls that will work. */
export const useCanRun = () => {
  const me = useContext(MeContext)
  return me?.role === 'operator' || me?.role === 'admin'
}
