-- A starter set of campuses across regions. LPU was the original pilot; any
-- student can request their own campus, which an admin then activates.
INSERT INTO campuses (slug, name, country, city, timezone, currency, email_domains, status) VALUES
    ('lpu',              'Lovely Professional University',           'India',          'Phagwara',   'Asia/Kolkata',        'INR', '{lpu.in,lpu.co.in}',                       'active'),
    ('iit-delhi',        'Indian Institute of Technology Delhi',     'India',          'New Delhi',  'Asia/Kolkata',        'INR', '{iitd.ac.in}',                             'active'),
    ('university-of-delhi', 'University of Delhi',                   'India',          'New Delhi',  'Asia/Kolkata',        'INR', '{du.ac.in}',                               'active'),
    ('stanford',         'Stanford University',                      'United States',  'Stanford',   'America/Los_Angeles', 'USD', '{stanford.edu}',                           'active'),
    ('mit',              'Massachusetts Institute of Technology',    'United States',  'Cambridge',  'America/New_York',    'USD', '{mit.edu}',                                'active'),
    ('university-of-toronto', 'University of Toronto',               'Canada',         'Toronto',    'America/Toronto',     'CAD', '{utoronto.ca}',                            'active'),
    ('oxford',           'University of Oxford',                     'United Kingdom', 'Oxford',     'Europe/London',       'GBP', '{ox.ac.uk}',                               'active'),
    ('tum',              'Technical University of Munich',           'Germany',        'Munich',     'Europe/Berlin',       'EUR', '{tum.de,mytum.de}',                        'active'),
    ('nus',              'National University of Singapore',         'Singapore',      'Singapore',  'Asia/Singapore',      'SGD', '{nus.edu.sg,u.nus.edu}',                   'active'),
    ('unimelb',          'University of Melbourne',                  'Australia',      'Melbourne',  'Australia/Melbourne', 'AUD', '{unimelb.edu.au}',                         'active'),
    ('u-tokyo',          'The University of Tokyo',                  'Japan',          'Tokyo',      'Asia/Tokyo',          'JPY', '{u-tokyo.ac.jp}',                          'active'),
    ('unilag',           'University of Lagos',                      'Nigeria',        'Lagos',      'Africa/Lagos',        'NGN', '{unilag.edu.ng}',                          'active'),
    ('uct',              'University of Cape Town',                  'South Africa',   'Cape Town',  'Africa/Johannesburg', 'ZAR', '{uct.ac.za,myuct.ac.za}',                  'active'),
    ('usp',              'University of São Paulo',                  'Brazil',         'São Paulo',  'America/Sao_Paulo',   'BRL', '{usp.br}',                                 'active');
