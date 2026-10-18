import { createContext, useContext, useMemo, type ReactNode } from 'react'
import { useQuery } from './hooks'
import { enumLabel } from './format'

// The lists of choices (guide services, session types, community types,
// report reasons, post types, session lengths, interview types, scorecard
// criteria, resume sections) come from the backend, which
// also validates them, so the web app never keeps its own copy.

export interface Option {
  value: string
  label: string
}

export interface Catalog {
  guideServices: Option[]
  sessionModes: Option[]
  sessionDurations: number[]
  communityKinds: Option[]
  reportReasons: Option[]
  postTypes: Option[]
  interviewTypes: Option[]
  feedbackCriteria: Option[]
  interviewVerdicts: Option[]
  resumeSections: Option[]
  codeLanguages: Option[]
}

type ListName = Exclude<keyof Catalog, 'sessionDurations'>

const empty: Catalog = {
  guideServices: [], sessionModes: [], sessionDurations: [], communityKinds: [], reportReasons: [], postTypes: [],
  interviewTypes: [], feedbackCriteria: [], interviewVerdicts: [], resumeSections: [], codeLanguages: [],
}

const CatalogContext = createContext<Catalog>(empty)

export function CatalogProvider({ children }: { children: ReactNode }) {
  const { data } = useQuery<{ catalog: Catalog }>(
    `{ catalog {
        guideServices { value label } sessionModes { value label } sessionDurations
        communityKinds { value label } reportReasons { value label } postTypes { value label }
        interviewTypes { value label } feedbackCriteria { value label } interviewVerdicts { value label } resumeSections { value label }
        codeLanguages { value label } } }`,
  )
  return <CatalogContext.Provider value={data?.catalog ?? empty}>{children}</CatalogContext.Provider>
}

/** The catalog, plus label() to name a value from one of its lists. */
export function useCatalog() {
  const catalog = useContext(CatalogContext)
  return useMemo(() => {
    const maps = Object.fromEntries(
      (Object.keys(catalog) as (keyof Catalog)[])
        .filter((k): k is ListName => k !== 'sessionDurations')
        .map((k) => [k, new Map(catalog[k].map((o) => [o.value, o.label]))]),
    ) as Record<ListName, Map<string, string>>
    // Until the catalog arrives, fall back to a readable form of the value.
    const label = (list: ListName, value: string) => maps[list].get(value) ?? enumLabel(value)
    return { ...catalog, label }
  }, [catalog])
}
