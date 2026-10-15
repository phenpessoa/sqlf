SELECT "id",
       CASE
           WHEN "source" = 'ads'        THEN 'paid'
           WHEN "source" = 'organic'    THEN 'free'
           ELSE 'other'
       END AS "channel"
  FROM "whatsapp_tracking";
