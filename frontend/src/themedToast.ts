import { toast } from '@zerodevx/svelte-toast'

export const success = (m: string) => toast.push(m, {
  theme: {
    '--toastBackground': 'green',
    '--toastColor': 'white',
    '--toastBarBackground': 'olive'
  }
})

export const warning = (m: string) => toast.push(m, {
  theme: {
    '--toastColor': 'orange',
    '--toastBarBackground': 'white',
    '--toastBarColor': 'orange',
  }
})

export const failure = (m: string) => toast.push(m, {
  theme: {
    '--toastColor': 'red',
    '--toastBarBackground': 'white',
    '--toastBarColor': 'red',
  }
})